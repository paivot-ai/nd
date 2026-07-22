package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAssetName(t *testing.T) {
	cases := []struct {
		tag, goos, goarch, want string
		wantErr                 bool
	}{
		{"v0.11.0", "darwin", "arm64", "nd_0.11.0_darwin_arm64.tar.gz", false},
		{"v0.11.0", "linux", "amd64", "nd_0.11.0_linux_amd64.tar.gz", false},
		{"v0.11.0", "windows", "amd64", "nd_0.11.0_windows_amd64.zip", false},
		{"v0.11.0", "plan9", "amd64", "", true},
		{"v0.11.0", "linux", "riscv64", "", true},
	}
	for _, c := range cases {
		got, err := assetName(c.tag, c.goos, c.goarch)
		if c.wantErr {
			if err == nil {
				t.Errorf("assetName(%s/%s): expected error", c.goos, c.goarch)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("assetName(%s, %s, %s) = %q, %v; want %q", c.tag, c.goos, c.goarch, got, err, c.want)
		}
	}
}

func TestResolveTagNormalization(t *testing.T) {
	tag, err := resolveTag(Options{Version: "0.11.0"})
	if err != nil || tag != "v0.11.0" {
		t.Errorf("resolveTag(0.11.0) = %q, %v; want v0.11.0", tag, err)
	}
	tag, err = resolveTag(Options{Version: "v0.11.0"})
	if err != nil || tag != "v0.11.0" {
		t.Errorf("resolveTag(v0.11.0) = %q, %v; want v0.11.0", tag, err)
	}
	if _, err := resolveTag(Options{Version: "not a tag"}); err == nil {
		t.Error("expected invalid tag to be rejected")
	}
}

func TestChecksumForAsset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	content := "abc123  nd_0.11.0_linux_amd64.tar.gz\ndef456  *nd_0.11.0_darwin_arm64.tar.gz\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := checksumForAsset(path, "nd_0.11.0_darwin_arm64.tar.gz")
	if err != nil || got != "def456" {
		t.Errorf("checksumForAsset = %q, %v; want def456", got, err)
	}
	if _, err := checksumForAsset(path, "missing.tar.gz"); err == nil {
		t.Error("expected missing asset to error")
	}
}

// buildRelease creates a fake goreleaser release: a tar.gz containing an nd
// binary (a script that fakes --version), plus checksums.txt content.
func buildRelease(t *testing.T, version string) (archive []byte, checksums string, assetFile string) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\necho \"nd version %s\"\n", version)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "nd", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	assetFile, err := assetName("v"+version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("platform unsupported for upgrade test: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	checksums = fmt.Sprintf("%x  %s\n", sum, assetFile)
	return buf.Bytes(), checksums, assetFile
}

func TestRunEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script-based fake binary requires a POSIX shell")
	}
	const version = "9.9.9"
	archive, checksums, assetFile := buildRelease(t, version)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/paivot-ai/nd/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v%s"}`, version)
	})
	mux.HandleFunc("/paivot-ai/nd/releases/download/v"+version+"/"+assetFile, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/paivot-ai/nd/releases/download/v"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	installDir := t.TempDir()
	var ranCommands []string
	res, err := Run(Options{
		Version:    "latest",
		InstallDir: installDir,
		Out:        io.Discard,
		GithubBase: srv.URL,
		APIBase:    srv.URL,
		HomeDir:    t.TempDir(), // no plugin cache -> no warnings expected
		Run: func(name string, args ...string) (string, error) {
			ranCommands = append(ranCommands, name+" "+strings.Join(args, " "))
			if strings.HasSuffix(name, "nd-candidate") && len(args) == 1 && args[0] == "--version" {
				return "nd version " + version, nil
			}
			return "", nil
		},
		LookPath: func(name string) (string, error) {
			return "", fmt.Errorf("%s not found", name) // no agent hosts detected
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Tag != "v"+version || !res.BinaryUpdated {
		t.Fatalf("unexpected result: %+v", res)
	}

	installed := filepath.Join(installDir, "nd")
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("installed binary missing: %v", err)
	}
	if !strings.Contains(string(data), version) {
		t.Error("installed binary is not the downloaded candidate")
	}
	info, _ := os.Stat(installed)
	if info.Mode()&0o111 == 0 {
		t.Error("installed binary is not executable")
	}
}

func TestRunChecksumMismatchRefusesInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script-based fake binary requires a POSIX shell")
	}
	const version = "9.9.8"
	archive, _, assetFile := buildRelease(t, version)

	mux := http.NewServeMux()
	mux.HandleFunc("/paivot-ai/nd/releases/download/v"+version+"/"+assetFile, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/paivot-ai/nd/releases/download/v"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%064d  %s\n", 0, assetFile) // wrong checksum
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	installDir := t.TempDir()
	_, err := Run(Options{
		Version:    version,
		InstallDir: installDir,
		Out:        io.Discard,
		GithubBase: srv.URL,
		APIBase:    srv.URL,
		HomeDir:    t.TempDir(),
		LookPath:   func(name string) (string, error) { return "", fmt.Errorf("not found") },
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nd")); statErr == nil {
		t.Error("binary must not be installed on checksum failure")
	}
}

func TestRunVersionMismatchRefusesInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script-based fake binary requires a POSIX shell")
	}
	const version = "9.9.7"
	archive, checksums, assetFile := buildRelease(t, version)

	mux := http.NewServeMux()
	mux.HandleFunc("/paivot-ai/nd/releases/download/v"+version+"/"+assetFile, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/paivot-ai/nd/releases/download/v"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	installDir := t.TempDir()
	_, err := Run(Options{
		Version:    version,
		InstallDir: installDir,
		Out:        io.Discard,
		GithubBase: srv.URL,
		APIBase:    srv.URL,
		HomeDir:    t.TempDir(),
		Run: func(name string, args ...string) (string, error) {
			return "nd version 0.0.1", nil // binary lies about its version
		},
		LookPath: func(name string) (string, error) { return "", fmt.Errorf("not found") },
	})
	if err == nil || !strings.Contains(err.Error(), "reports") {
		t.Fatalf("expected version validation failure, got %v", err)
	}
}

func TestCheckModeChangesNothing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/paivot-ai/nd/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name": "v9.9.9"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var out bytes.Buffer
	res, err := Run(Options{
		Check:          true,
		CurrentVersion: "0.10.21",
		Out:            &out,
		GithubBase:     srv.URL,
		APIBase:        srv.URL,
		HomeDir:        t.TempDir(),
		LookPath:       func(name string) (string, error) { return "", fmt.Errorf("not found") },
	})
	if err != nil {
		t.Fatalf("Run --check: %v", err)
	}
	if res.BinaryUpdated || res.PluginUpdates != 0 {
		t.Error("check mode must not change anything")
	}
	if !strings.Contains(out.String(), "v9.9.9") {
		t.Errorf("check output missing latest version: %q", out.String())
	}
}

func TestSkillRefreshUsesHostCLIs(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins", "cache", "nd"), 0o755); err != nil {
		t.Fatal(err)
	}

	var calls []string
	updates, warnings := refreshAgentSkills(Options{
		HomeDir: home,
		Run: func(name string, args ...string) (string, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			if name == "codex" && args[0] == "plugin" && args[1] == "list" {
				return "nd@nd 0.10.21", nil
			}
			return "", nil
		},
		LookPath: func(name string) (string, error) { return name, nil },
	}, io.Discard)

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	// 3 claude scopes + 1 codex refresh
	if updates != 4 {
		t.Errorf("updates = %d, want 4 (calls: %v)", updates, calls)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"claude plugin marketplace update nd",
		"claude plugin update nd@nd --scope user",
		"codex plugin add nd@nd",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing host CLI call %q in:\n%s", want, joined)
		}
	}
}
