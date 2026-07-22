package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var version = "dev"

var (
	vaultDir string
	jsonOut  bool
	verbose  bool
	quiet    bool
)

var rootCmd = &cobra.Command{
	Use:          "nd",
	Short:        "Vault-backed issue tracker",
	Long:         "nd -- Git-native issue tracking with Obsidian-compatible markdown files.",
	Version:      version,
	SilenceUsage: true,
	// After any successful mutating command, snapshot the vault to the
	// backlog branch (best effort, disabled via sync.auto=off or
	// ND_SYNC_AUTO=off). This keeps the branch a continuous journal: a wiped
	// vault loses at most the current command.
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if mutatingCommandPaths[cmd.CommandPath()] {
			autoSnapshot(cmd.CommandPath())
		}
	},
}

// mutatingCommandPaths lists the command paths whose success changes vault
// state and therefore warrants an auto-snapshot. Read-only commands and sync
// itself are deliberately absent.
var mutatingCommandPaths = map[string]bool{
	"nd init":         true,
	"nd create":       true,
	"nd q":            true,
	"nd update":       true,
	"nd edit":         true,
	"nd close":        true,
	"nd reopen":       true,
	"nd defer":        true,
	"nd undefer":      true,
	"nd delete":       true,
	"nd migrate":      true,
	"nd import":       true,
	"nd claim":        true,
	"nd release":      true,
	"nd start":        true,
	"nd block":        true,
	"nd unblock":      true,
	"nd resolve":      true,
	"nd dep add":      true,
	"nd dep rm":       true,
	"nd dep relate":   true,
	"nd dep unrelate": true,
	"nd labels add":   true,
	"nd labels rm":    true,
	"nd comments add": true,
	"nd comment add":  true,
	"nd config set":   true,
	"nd doctor":       true, // no-op snapshot unless --fix changed something
	"nd archive":      true, // mutates only with --remove-archived; no-op otherwise
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&vaultDir, "vault", "", "vault directory (default: .vault)")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output as JSON")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "suppress non-essential output")
}

const sharedVaultConfigRelPath = ".vault/.nd-shared.yaml"

// Conventional shared vault location under the git common dir, written by
// `pvg nd root --ensure`. Used only as a convergence fallback when no
// checkout-visible config exists.
const sharedVaultDefaultRelPath = "paivot/nd-vault"

// resolveVaultDir returns the nd vault directory and warns (once, on stderr)
// when a local .vault is being shadowed by the resolved vault -- typically a
// stale gitignored copy left behind in a secondary git worktree.
func resolveVaultDir() string {
	resolved := resolveVaultDirPath()
	maybeWarnVaultDivergence(resolved)
	return resolved
}

// resolveVaultDirPath returns the nd vault directory.
//
// Repos that opt into shared worktree state via `.vault/.nd-shared.yaml` resolve
// their live vault from the repository's git common dir. Other repos fall back
// to the nearest local `.vault`. When running inside a secondary git worktree
// where `.vault` is gitignored and absent, the resolver falls back to the main
// worktree's `.vault`.
func resolveVaultDirPath() string {
	if vaultDir != "" {
		return vaultDir
	}
	if override := strings.TrimSpace(os.Getenv("ND_VAULT_DIR")); override != "" {
		return filepath.Clean(override)
	}

	dir, err := os.Getwd()
	if err != nil {
		errorf("cannot determine working directory: %v", err)
		return ".vault"
	}

	if path, err := resolveSharedVaultDir(dir); err == nil {
		return path
	}

	// Convergence fallbacks for linked worktrees and stale branches whose
	// checkout does not (yet) carry the tracked shared config. Without
	// these, agents in different worktrees resolve divergent vault views.
	if commonDir, err := gitCommonDir(dir); err == nil {
		// The main checkout may hold the config even when this worktree
		// branched before it was committed.
		if filepath.Base(commonDir) == ".git" {
			mainConfig := filepath.Join(filepath.Dir(commonDir), sharedVaultConfigRelPath)
			if _, statErr := os.Stat(mainConfig); statErr == nil {
				if mode, relPath, perr := parseSharedVaultConfig(mainConfig); perr == nil && mode == "git_common_dir" {
					return filepath.Join(commonDir, relPath)
				}
			}
		}
		// Or the shared vault may already be initialized even when no
		// visible checkout carries the config.
		shared := filepath.Join(commonDir, sharedVaultDefaultRelPath)
		if _, statErr := os.Stat(filepath.Join(shared, ".nd.yaml")); statErr == nil {
			return shared
		}
	}

	if path, err := resolveLocalVaultDir(dir); err == nil {
		return path
	}

	// Worktree fallback: if we are in a secondary git worktree where
	// .vault is gitignored (and therefore absent), resolve .vault from
	// the main worktree instead.
	if path, err := resolveMainWorktreeVault(dir); err == nil {
		return path
	}

	return ".vault"
}

// vaultDivergenceWarned ensures the divergence warning fires at most once per
// invocation, not once per store operation.
var vaultDivergenceWarned bool

// maybeWarnVaultDivergence warns on stderr when a local .vault candidate
// exists but a different vault was resolved (shared vault or main-worktree
// fallback). A stale gitignored .vault/ in a secondary worktree still carries
// old issues/ copies that mislead anyone reading the files directly. The
// warning goes to stderr only -- JSON consumers read stdout -- and is
// suppressed by the global --quiet flag.
func maybeWarnVaultDivergence(resolved string) {
	if quiet || vaultDivergenceWarned {
		return
	}
	vaultDivergenceWarned = true

	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	local, err := resolveLocalVaultDir(cwd)
	if err != nil {
		return
	}
	if vaultDivergence(resolved, local) {
		errorf("using vault %s; ignoring local .vault at %s (stale worktree copy?)", resolved, local)
	}
}

// vaultDivergence reports whether the local .vault candidate points somewhere
// other than the resolved vault. Paths are compared after symlink resolution
// when resolvable, falling back to lexical cleaning.
func vaultDivergence(resolved, localCandidate string) bool {
	if resolved == "" || localCandidate == "" {
		return false
	}
	return canonicalPath(resolved) != canonicalPath(localCandidate)
}

// canonicalPath normalizes a path for comparison: absolute, symlinks resolved
// when possible, lexically cleaned otherwise.
func canonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

func parentDir(s string) string {
	parent := filepath.Dir(s)
	if parent == "." {
		return s
	}
	return parent
}

func errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nd: "+format+"\n", args...)
}

func resolveSharedVaultDir(start string) (string, error) {
	configPath, root, err := findSharedVaultConfig(start)
	if err != nil {
		return "", err
	}

	mode, relPath, err := parseSharedVaultConfig(configPath)
	if err != nil {
		return "", err
	}
	if mode != "git_common_dir" {
		return "", fmt.Errorf("unsupported shared nd mode %q", mode)
	}

	commonDir, err := gitCommonDir(root)
	if err != nil {
		return "", err
	}

	return filepath.Join(commonDir, relPath), nil
}

func findSharedVaultConfig(start string) (path, root string, err error) {
	dir := filepath.Clean(start)
	for {
		candidate := filepath.Join(dir, sharedVaultConfigRelPath)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, dir, nil
		}

		parent := parentDir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", "", fmt.Errorf("no shared nd config found")
}

func parseSharedVaultConfig(path string) (mode, relPath string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "mode":
			mode = value
		case "path":
			relPath = filepath.Clean(value)
		}
	}

	if mode == "" || relPath == "" {
		return "", "", fmt.Errorf("invalid shared nd config %s", path)
	}
	if filepath.IsAbs(relPath) || relPath == "." || relPath == "" {
		return "", "", fmt.Errorf("invalid shared nd path %q", relPath)
	}
	return mode, relPath, nil
}

func resolveLocalVaultDir(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		candidate := filepath.Join(dir, ".vault")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			// Only accept a .vault that actually contains .nd.yaml.
			// Worktree checkouts may contain a skeletal .vault/ with
			// only tracked files (.gitignore) but no nd state.
			if _, ndErr := os.Stat(filepath.Join(candidate, ".nd.yaml")); ndErr == nil {
				return candidate, nil
			}
		}
		parent := parentDir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no .vault found")
}

func gitCommonDir(start string) (string, error) {
	repoRoot, err := findRepoRoot(start)
	if err != nil {
		return "", err
	}

	gitPath := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return filepath.Clean(gitPath), nil
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}

	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("%s does not contain a gitdir pointer", gitPath)
	}

	gitDir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	gitDir = filepath.Clean(gitDir)

	commonDirPath := filepath.Join(gitDir, "commondir")
	if data, err := os.ReadFile(commonDirPath); err == nil {
		commonDir := strings.TrimSpace(string(data))
		if commonDir != "" {
			if !filepath.IsAbs(commonDir) {
				commonDir = filepath.Join(gitDir, commonDir)
			}
			return filepath.Clean(commonDir), nil
		}
	}

	return gitDir, nil
}

// resolveMainWorktreeVault finds the .vault in the main worktree when
// running inside a secondary git worktree. Secondary worktrees have a
// .git file (not directory) that points to .git/worktrees/<name>. By
// following the commondir pointer we locate the main repo and its .vault.
func resolveMainWorktreeVault(start string) (string, error) {
	repoRoot, err := findRepoRoot(start)
	if err != nil {
		return "", err
	}

	gitPath := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("not a git worktree")
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("not a gitdir pointer")
	}

	gitDir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	gitDir = filepath.Clean(gitDir)

	commonData, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return "", err
	}
	commonDir := strings.TrimSpace(string(commonData))
	if commonDir == "" {
		return "", fmt.Errorf("empty commondir")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitDir, commonDir)
	}
	commonDir = filepath.Clean(commonDir)

	// Main worktree root is the parent of the .git directory.
	mainRoot := filepath.Dir(commonDir)
	candidate := filepath.Join(mainRoot, ".vault")
	if vaultInfo, statErr := os.Stat(candidate); statErr == nil && vaultInfo.IsDir() {
		return candidate, nil
	}

	return "", fmt.Errorf("no .vault in main worktree %s", mainRoot)
}

func findRepoRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := parentDir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no git repo found")
}
