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

// resolveVaultDir returns the nd vault directory.
//
// Repos that opt into shared worktree state via `.vault/.nd-shared.yaml` resolve
// their live vault from the repository's git common dir. Other repos fall back
// to the nearest local `.vault`. When running inside a secondary git worktree
// where `.vault` is gitignored and absent, the resolver falls back to the main
// worktree's `.vault`.
func resolveVaultDir() string {
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
