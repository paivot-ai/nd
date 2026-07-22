// Package selfupdate implements `nd upgrade`: download the requested GitHub
// release, verify its published SHA-256 checksum, validate the binary's
// reported version, atomically replace the running executable, then refresh
// the nd skills in every detected agent host (Claude Code, Codex) through
// their own plugin managers. Host-owned plugin caches are never edited
// directly.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultRepo is the canonical source repository.
	DefaultRepo = "paivot-ai/nd"

	defaultGithubBase = "https://github.com"
	defaultAPIBase    = "https://api.github.com"

	checksumsAsset = "checksums.txt"
)

var releaseTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+([.-].+)?$`)

// Options configures an upgrade run.
type Options struct {
	Version        string // "", "latest", or an explicit tag ("v0.11.0" / "0.11.0")
	Repo           string // owner/name; default DefaultRepo
	InstallDir     string // binary destination directory; default: replace the running executable
	CurrentVersion string // version of the running binary, for --check reporting
	Check          bool   // report current vs latest and stop
	BinaryOnly     bool   // skip the skill/plugin refresh
	SkillsOnly     bool   // skip the binary replacement
	Out            io.Writer

	// Test seams. Zero values use the real implementations.
	GithubBase string
	APIBase    string
	Run        func(name string, args ...string) (string, error)
	LookPath   func(name string) (string, error)
	HomeDir    string
}

// Result summarizes a completed upgrade.
type Result struct {
	Tag           string
	Executable    string
	BinaryUpdated bool
	PluginUpdates int
	Warnings      []string
}

// Run executes the upgrade.
func Run(opts Options) (*Result, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.GithubBase == "" {
		opts.GithubBase = defaultGithubBase
	}
	if opts.APIBase == "" {
		opts.APIBase = defaultAPIBase
	}
	if opts.Run == nil {
		opts.Run = runCombined
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.HomeDir == "" {
		opts.HomeDir, _ = os.UserHomeDir()
	}

	tag, err := resolveTag(opts)
	if err != nil {
		return nil, err
	}
	res := &Result{Tag: tag}

	if opts.Check {
		current := strings.TrimPrefix(strings.TrimSpace(opts.CurrentVersion), "v")
		latest := strings.TrimPrefix(tag, "v")
		if current == latest {
			fmt.Fprintf(out, "nd %s is current (latest release: %s)\n", current, tag)
		} else {
			fmt.Fprintf(out, "nd %s installed; %s available -- run `nd upgrade`\n", current, tag)
		}
		return res, nil
	}

	if !opts.SkillsOnly {
		exe, err := upgradeBinary(opts, tag, out)
		if err != nil {
			return nil, err
		}
		res.Executable = exe
		res.BinaryUpdated = true
	}

	if !opts.BinaryOnly {
		updates, warnings := refreshAgentSkills(opts, out)
		res.PluginUpdates = updates
		res.Warnings = warnings
	}

	fmt.Fprintf(out, "nd upgrade complete: %s (binary updated: %t, plugin refreshes: %d)\n",
		tag, res.BinaryUpdated, res.PluginUpdates)
	if res.PluginUpdates > 0 {
		fmt.Fprintln(out, "restart Claude Code sessions to pick up the refreshed skills")
	}
	return res, nil
}

// resolveTag returns an explicit tag (normalized to a leading v), otherwise
// the newest published release.
func resolveTag(opts Options) (string, error) {
	v := strings.TrimSpace(opts.Version)
	if v != "" && v != "latest" {
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		if !releaseTagRe.MatchString(v) {
			return "", fmt.Errorf("invalid release tag %q", opts.Version)
		}
		return v, nil
	}

	data, err := httpGet(opts.APIBase + "/repos/" + opts.Repo + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("resolve latest release for %s: %w", opts.Repo, err)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return "", fmt.Errorf("parse release metadata: %w", err)
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("no published release found for %s", opts.Repo)
	}
	return rel.TagName, nil
}

// upgradeBinary downloads, verifies, validates, and installs the release
// binary, returning the destination path.
func upgradeBinary(opts Options, tag string, out io.Writer) (string, error) {
	asset, err := assetName(tag, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp("", "nd-upgrade-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	base := opts.GithubBase + "/" + opts.Repo + "/releases/download/" + tag
	fmt.Fprintf(out, "fetching nd %s (%s/%s)\n", tag, runtime.GOOS, runtime.GOARCH)

	archivePath := filepath.Join(tmp, asset)
	if err := downloadFile(base+"/"+asset, archivePath); err != nil {
		return "", fmt.Errorf("download %s: %w", asset, err)
	}
	checksumsPath := filepath.Join(tmp, checksumsAsset)
	if err := downloadFile(base+"/"+checksumsAsset, checksumsPath); err != nil {
		return "", fmt.Errorf("release %s has no %s: %w", tag, checksumsAsset, err)
	}
	if err := verifyChecksum(checksumsPath, archivePath, asset); err != nil {
		return "", err
	}
	fmt.Fprintln(out, "checksum verified")

	candidate, err := extractBinary(archivePath, tmp)
	if err != nil {
		return "", err
	}
	if err := validateBinary(candidate, tag, opts.Run); err != nil {
		return "", err
	}

	destination := ""
	if opts.InstallDir != "" {
		name := "nd"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		destination = filepath.Join(opts.InstallDir, name)
	} else {
		destination, err = os.Executable()
		if err != nil {
			return "", err
		}
	}
	if abs, aerr := filepath.Abs(destination); aerr == nil {
		destination = abs
	}
	if resolved, rerr := filepath.EvalSymlinks(destination); rerr == nil {
		destination = resolved
	}

	if err := replaceExecutable(candidate, destination); err != nil {
		return "", fmt.Errorf("replace nd binary at %s: %w", destination, err)
	}
	fmt.Fprintf(out, "updated nd binary -> %s (%s)\n", destination, tag)
	return destination, nil
}

// assetName maps a tag and platform to the goreleaser archive name:
// nd_<version>_<os>_<arch>.tar.gz (zip on windows).
func assetName(tag, goos, goarch string) (string, error) {
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("unsupported architecture for upgrade: %s", goarch)
	}
	version := strings.TrimPrefix(tag, "v")
	base := fmt.Sprintf("nd_%s_%s_%s", version, goos, goarch)
	switch goos {
	case "darwin", "linux":
		return base + ".tar.gz", nil
	case "windows":
		return base + ".zip", nil
	default:
		return "", fmt.Errorf("unsupported operating system for upgrade: %s", goos)
	}
}

func verifyChecksum(checksumsPath, archivePath, asset string) error {
	want, err := checksumForAsset(checksumsPath, asset)
	if err != nil {
		return err
	}
	got, err := fileSHA256(archivePath)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("checksum mismatch for %s (want %s, got %s)", asset, want, got)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func checksumForAsset(path, asset string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", asset)
}

// extractBinary pulls the nd binary out of a goreleaser tar.gz or zip archive.
func extractBinary(archivePath, dir string) (string, error) {
	want := "nd"
	if strings.HasSuffix(archivePath, ".zip") {
		want = "nd.exe"
	}
	dest := filepath.Join(dir, "nd-candidate")

	if strings.HasSuffix(archivePath, ".zip") {
		r, err := zip.OpenReader(archivePath)
		if err != nil {
			return "", err
		}
		defer r.Close()
		for _, f := range r.File {
			if filepath.Base(f.Name) != want {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			return dest, writeFile(dest, rc, 0o755)
		}
		return "", fmt.Errorf("archive %s does not contain %s", filepath.Base(archivePath), want)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != want {
			continue
		}
		return dest, writeFile(dest, tr, 0o755)
	}
	return "", fmt.Errorf("archive %s does not contain %s", filepath.Base(archivePath), want)
}

func writeFile(dest string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// validateBinary confirms the downloaded binary reports the requested
// version before it replaces anything.
func validateBinary(candidate, tag string, run func(string, ...string) (string, error)) error {
	output, err := run(candidate, "--version")
	if err != nil {
		return fmt.Errorf("validate downloaded binary: %w (%s)", err, strings.TrimSpace(output))
	}
	version := strings.TrimPrefix(tag, "v")
	got := strings.TrimSpace(output)
	if !strings.Contains(got, version) {
		return fmt.Errorf("downloaded binary reports %q, want version %s", got, version)
	}
	return nil
}

// replaceExecutable installs candidate over destination atomically: staged
// copy in the destination directory, fsync, rename.
func replaceExecutable(candidate, destination string) error {
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	in, err := os.Open(candidate)
	if err != nil {
		return err
	}
	defer in.Close()

	staged, err := os.CreateTemp(dir, ".nd-upgrade-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := io.Copy(staged, in); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Chmod(0o755); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	return os.Rename(stagedPath, destination)
}

// refreshAgentSkills refreshes the nd plugin in every detected agent host
// through that host's own plugin manager. Never edits caches directly.
func refreshAgentSkills(opts Options, out io.Writer) (int, []string) {
	updates := 0
	var warnings []string
	warn := func(msg string) {
		warnings = append(warnings, msg)
		fmt.Fprintln(out, "warning: "+msg)
	}

	claudeCachePresent := false
	if opts.HomeDir != "" {
		if _, err := os.Stat(filepath.Join(opts.HomeDir, ".claude", "plugins", "cache", "nd")); err == nil {
			claudeCachePresent = true
		}
	}

	if claude, err := opts.LookPath("claude"); err == nil {
		_, _ = opts.Run(claude, "plugin", "marketplace", "update", "nd")
		claudeUpdates := 0
		var failures []string
		for _, scope := range []string{"user", "project", "local"} {
			if output, err := opts.Run(claude, "plugin", "update", "nd@nd", "--scope", scope); err != nil {
				if text := strings.TrimSpace(output); text != "" {
					failures = append(failures, scope+": "+text)
				}
				continue
			}
			claudeUpdates++
		}
		if claudeUpdates == 0 && claudeCachePresent {
			detail := strings.Join(failures, "; ")
			if detail != "" {
				detail = " (" + detail + ")"
			}
			warn("Claude Code plugin refresh failed; run `claude plugin update nd@nd`" + detail)
		} else if claudeUpdates > 0 {
			updates += claudeUpdates
			fmt.Fprintf(out, "refreshed Claude Code plugin nd@nd in %d scope(s)\n", claudeUpdates)
		}
	} else if claudeCachePresent {
		warn("nd Claude Code plugin detected, but `claude` is not on PATH; run `claude plugin update nd@nd`")
	}

	if codex, err := opts.LookPath("codex"); err == nil {
		listed, listErr := opts.Run(codex, "plugin", "list")
		if listErr == nil && strings.Contains(strings.ToLower(listed), "nd@nd") {
			if output, err := opts.Run(codex, "plugin", "add", "nd@nd"); err != nil {
				warn("Codex nd plugin refresh failed; run `codex plugin add nd@nd` (" + strings.TrimSpace(output) + ")")
			} else {
				updates++
				fmt.Fprintln(out, "refreshed Codex plugin nd@nd")
			}
		}
	}

	return updates, warnings
}

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "nd-upgrade")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "nd-upgrade")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return writeFile(dest, resp.Body, 0o644)
}

func runCombined(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
