package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveVaultDir_PrefersSharedVaultForConfiguredWorktree(t *testing.T) {
	projectRoot, sharedVault := setupSharedWorktree(t)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}

	oldVault := vaultDir
	vaultDir = ""
	defer func() { vaultDir = oldVault }()

	if got := resolveVaultDir(); got != sharedVault {
		t.Fatalf("resolveVaultDir() = %q, want %q", got, sharedVault)
	}
}

func TestResolveVaultDir_FallsBackToNearestLocalVault(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "repo")
	nested := filepath.Join(projectRoot, "pkg", "service")

	if err := os.MkdirAll(filepath.Join(projectRoot, ".vault"), 0o755); err != nil {
		t.Fatal(err)
	}
	// resolveLocalVaultDir requires .nd.yaml to exist (not just the directory)
	if err := os.WriteFile(filepath.Join(projectRoot, ".vault", ".nd.yaml"), []byte("version: \"1\"\nprefix: TEST\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	oldVault := vaultDir
	vaultDir = ""
	defer func() { vaultDir = oldVault }()

	want := filepath.Join(projectRoot, ".vault")
	got := resolveVaultDir()
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if got != want {
		t.Fatalf("resolveVaultDir() = %q, want %q", got, want)
	}
}

func TestResolveVaultDir_UsesEnvironmentOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "override-vault")
	if err := os.Setenv("ND_VAULT_DIR", override); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Unsetenv("ND_VAULT_DIR") }()

	oldVault := vaultDir
	vaultDir = ""
	defer func() { vaultDir = oldVault }()

	if got := resolveVaultDir(); got != override {
		t.Fatalf("resolveVaultDir() = %q, want %q", got, override)
	}
}

// TestResolveVaultDir_WorktreeFallbackFindsMainVault verifies that when
// running inside a secondary git worktree where .vault is gitignored (absent),
// resolveVaultDir falls back to the main worktree's .vault.
func TestResolveVaultDir_WorktreeFallbackFindsMainVault(t *testing.T) {
	base := t.TempDir()

	// Main repo with .vault
	mainRepo := filepath.Join(base, "main-repo")
	mainGitDir := filepath.Join(mainRepo, ".git")
	mainVault := filepath.Join(mainRepo, ".vault")
	if err := os.MkdirAll(mainGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mainVault, 0o755); err != nil {
		t.Fatal(err)
	}

	// Secondary worktree -- NO .vault (gitignored), .git is a file
	worktree := filepath.Join(mainRepo, ".claude", "worktrees", "agent-abc")
	worktreeGitDir := filepath.Join(mainGitDir, "worktrees", "agent-abc")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreeGitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Worktree .git file points to the worktree git dir
	gitPtr := "gitdir: " + filepath.ToSlash(worktreeGitDir) + "\n"
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte(gitPtr), 0o644); err != nil {
		t.Fatal(err)
	}
	// commondir points back to the main .git
	if err := os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../../\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(worktree); err != nil {
		t.Fatal(err)
	}

	oldVault := vaultDir
	vaultDir = ""
	defer func() { vaultDir = oldVault }()

	got := resolveVaultDir()
	if resolved, rerr := filepath.EvalSymlinks(got); rerr == nil {
		got = resolved
	}
	want := mainVault
	if resolved, rerr := filepath.EvalSymlinks(want); rerr == nil {
		want = resolved
	}
	if got != want {
		t.Fatalf("resolveVaultDir() from worktree = %q, want main vault %q", got, want)
	}
}

func setupSharedWorktree(t *testing.T) (projectRoot, sharedVault string) {
	t.Helper()

	base := t.TempDir()
	projectRoot = filepath.Join(base, "repo")
	gitDir := filepath.Join(base, "gitdir", "worktrees", "story")
	commonDir := filepath.Join(base, "gitdir")
	sharedVault = filepath.Join(commonDir, "shared", "nd-vault")

	if err := os.MkdirAll(filepath.Join(projectRoot, ".vault"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "# nd shared-worktree state\nmode: git_common_dir\npath: shared/nd-vault\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".vault", ".nd-shared.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedVault, 0o755); err != nil {
		t.Fatal(err)
	}

	gitPtr := "gitdir: " + filepath.ToSlash(gitDir) + "\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".git"), []byte(gitPtr), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return projectRoot, sharedVault
}
