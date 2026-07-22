package gitsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/nd/internal/store"
)

// gitCmd runs git in dir, failing the test on error.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepoWithVault creates a git repo containing an initialized nd vault and
// returns (repoDir, vaultDir, store). The store holds the vault lock; tests
// close it before syncing from another handle.
func newRepoWithVault(t *testing.T, root, name, remote string) (string, string, *store.Store) {
	t.Helper()
	repo := filepath.Join(root, name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-q")
	gitCmd(t, repo, "config", "user.name", "test")
	gitCmd(t, repo, "config", "user.email", "test@test")
	if remote != "" {
		gitCmd(t, repo, "remote", "add", "origin", remote)
	}
	vault := filepath.Join(repo, ".vault")
	s, err := store.Init(vault, "T", "tester")
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}
	return repo, vault, s
}

func TestSnapshotAndIdempotency(t *testing.T) {
	root := t.TempDir()
	_, vault, s := newRepoWithVault(t, root, "a", "")
	if _, err := s.CreateIssue("First", "desc", "task", 2, "", nil, ""); err != nil {
		t.Fatal(err)
	}

	res, err := Snapshot(vault, DefaultBranch, "test snapshot", false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !res.Created {
		t.Fatal("first snapshot should create a commit")
	}

	res2, err := Snapshot(vault, DefaultBranch, "test snapshot 2", false)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if res2.Created {
		t.Error("unchanged vault must not create a second commit")
	}
	if res2.Commit != res.Commit {
		t.Error("idempotent snapshot must return the same tip")
	}

	// The snapshot must include issues even though .gitignore excludes them.
	r, err := DiscoverRepo(vault)
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.listTreeFiles(res.Commit)
	if err != nil {
		t.Fatal(err)
	}
	var hasIssue, hasConfig, hasLock bool
	for _, f := range files {
		if strings.HasPrefix(f, "issues/") && strings.HasSuffix(f, ".md") {
			hasIssue = true
		}
		if f == ".nd.yaml" {
			hasConfig = true
		}
		if f == ".vlt.lock" {
			hasLock = true
		}
	}
	if !hasIssue || !hasConfig {
		t.Errorf("snapshot tree missing issue or config: %v", files)
	}
	if hasLock {
		t.Error("snapshot must exclude runtime lock file")
	}
}

func TestSnapshotMassDeleteGuard(t *testing.T) {
	root := t.TempDir()
	_, vault, s := newRepoWithVault(t, root, "a", "")
	issue, err := s.CreateIssue("First", "desc", "task", 2, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(vault, DefaultBranch, "snap", false); err != nil {
		t.Fatal(err)
	}

	// Simulate a wiped issues/ directory (git clean on a naive layout).
	if err := os.Remove(filepath.Join(vault, "issues", issue.ID+".md")); err != nil {
		t.Fatal(err)
	}

	if _, err := Snapshot(vault, DefaultBranch, "snap after wipe", false); err == nil {
		t.Fatal("expected mass-delete guard to refuse the snapshot")
	}

	// Restore recovers the wiped issue.
	if _, err := Restore(vault, SyncOptions{}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "issues", issue.ID+".md")); err != nil {
		t.Fatal("restore did not bring the issue back")
	}

	// Force overrides the guard when deletion is intentional.
	if err := os.Remove(filepath.Join(vault, "issues", issue.ID+".md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(vault, DefaultBranch, "intentional purge", true); err != nil {
		t.Fatalf("forced snapshot: %v", err)
	}
}

func TestSyncPushRestoreAndMerge(t *testing.T) {
	root := t.TempDir()

	remote := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, remote, "init", "-q", "--bare")

	// Machine A: create backlog, sync (push).
	_, vaultA, sa := newRepoWithVault(t, root, "a", remote)
	i1, err := sa.CreateIssue("Login flow", "Build login", "feature", 1, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	i2, err := sa.CreateIssue("Signup flow", "Build signup", "feature", 2, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}

	resA, err := Sync(vaultA, SyncOptions{})
	if err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if !resA.Pushed {
		t.Fatal("first sync should push the new branch")
	}

	// Machine B: clone, restore the vault from the branch.
	repoB := filepath.Join(root, "b")
	gitCmd(t, root, "clone", "-q", remote, repoB)
	gitCmd(t, repoB, "config", "user.name", "test")
	gitCmd(t, repoB, "config", "user.email", "test@test")
	vaultB := filepath.Join(repoB, ".vault")
	if err := os.MkdirAll(vaultB, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(vaultB, SyncOptions{}); err != nil {
		t.Fatalf("restore B: %v", err)
	}

	sb, err := store.Open(vaultB)
	if err != nil {
		t.Fatalf("open restored vault: %v", err)
	}
	restored, err := sb.ReadIssue(i1.ID)
	if err != nil {
		t.Fatalf("restored vault missing %s: %v", i1.ID, err)
	}
	if restored.Title != "Login flow" {
		t.Errorf("restored title = %q", restored.Title)
	}

	// Diverge: A closes i1 and comments on i2; B claims i2 and creates i3.
	if err := sa.CloseIssue(i1.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := sa.AddComment(i2.ID, "notes from machine A"); err != nil {
		t.Fatal(err)
	}
	if _, err := sb.ClaimIssue(i2.ID, "agent-b", false); err != nil {
		t.Fatal(err)
	}
	i3, err := sb.CreateIssue("Password reset", "Build reset", "feature", 2, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// A pushes first.
	if _, err := Sync(vaultA, SyncOptions{}); err != nil {
		t.Fatalf("sync A after diverge: %v", err)
	}
	// B syncs: must fetch, three-way merge, materialize, push.
	resB, err := Sync(vaultB, SyncOptions{})
	if err != nil {
		t.Fatalf("sync B: %v", err)
	}
	if !resB.Merged {
		t.Fatal("expected a three-way merge on B")
	}
	// A pulls the merge.
	resA2, err := Sync(vaultA, SyncOptions{})
	if err != nil {
		t.Fatalf("final sync A: %v", err)
	}
	if !resA2.Pulled {
		t.Fatal("expected A to fast-forward to the merge")
	}

	// Both vaults must now agree on the merged truth.
	for name, st := range map[string]*store.Store{"A": sa, "B": sb} {
		closed, err := st.ReadIssue(i1.ID)
		if err != nil {
			t.Fatalf("%s: read %s: %v", name, i1.ID, err)
		}
		if closed.Status != model.StatusClosed {
			t.Errorf("%s: %s status = %s, want closed", name, i1.ID, closed.Status)
		}

		merged, err := st.ReadIssue(i2.ID)
		if err != nil {
			t.Fatalf("%s: read %s: %v", name, i2.ID, err)
		}
		if merged.Assignee != "agent-b" {
			t.Errorf("%s: %s assignee = %q, want agent-b (B's claim survives)", name, i2.ID, merged.Assignee)
		}
		if merged.Status != model.StatusInProgress {
			t.Errorf("%s: %s status = %s, want in_progress", name, i2.ID, merged.Status)
		}
		if !strings.Contains(merged.Body, "notes from machine A") {
			t.Errorf("%s: %s lost A's comment", name, i2.ID)
		}

		if _, err := st.ReadIssue(i3.ID); err != nil {
			t.Errorf("%s: missing issue %s created on B: %v", name, i3.ID, err)
		}
	}
}

func TestSyncWithoutRemoteIsLocalOnly(t *testing.T) {
	root := t.TempDir()
	_, vault, s := newRepoWithVault(t, root, "a", "")
	if _, err := s.CreateIssue("Solo", "d", "task", 2, "", nil, ""); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(vault, SyncOptions{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !res.RemoteAbsent || res.Pushed {
		t.Errorf("expected local-only sync, got %+v", res)
	}
	if !res.Snapshotted {
		t.Error("local-only sync must still snapshot")
	}
}

func TestMaterializePrunesToTrash(t *testing.T) {
	root := t.TempDir()
	_, vault, s := newRepoWithVault(t, root, "a", "")
	keep, err := s.CreateIssue("Keep", "d", "task", 2, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	snap1, err := Snapshot(vault, DefaultBranch, "one issue", false)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := s.CreateIssue("Extra", "d", "task", 2, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Materializing the older commit prunes the extra issue to trash.
	if err := Materialize(vault, snap1.Commit); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "issues", keep.ID+".md")); err != nil {
		t.Error("kept issue missing after materialize")
	}
	if _, err := os.Stat(filepath.Join(vault, "issues", extra.ID+".md")); err == nil {
		t.Error("pruned issue still in issues/")
	}
	if _, err := os.Stat(filepath.Join(vault, ".trash", extra.ID+".md")); err != nil {
		t.Error("pruned issue not preserved in .trash/")
	}
}

func TestSnapshotRefusesForeignBranch(t *testing.T) {
	root := t.TempDir()
	repo, vault, s := newRepoWithVault(t, root, "a", "")
	if _, err := s.CreateIssue("First", "d", "task", 2, "", nil, ""); err != nil {
		t.Fatal(err)
	}

	// A user's pre-existing branch that happens to be named nd/backlog.
	if err := os.WriteFile(filepath.Join(repo, "code.txt"), []byte("code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "code.txt")
	gitCmd(t, repo, "commit", "-q", "-m", "user commit")
	gitCmd(t, repo, "branch", DefaultBranch)

	if _, err := Snapshot(vault, DefaultBranch, "snap", false); err == nil {
		t.Fatal("expected snapshot onto a foreign branch to be refused")
	} else if !strings.Contains(err.Error(), "not an nd backlog branch") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Force takes the branch over deliberately.
	if _, err := Snapshot(vault, DefaultBranch, "snap", true); err != nil {
		t.Fatalf("forced snapshot: %v", err)
	}
}

func TestDiscoverRepoFromSharedVaultUnderGitDir(t *testing.T) {
	root := t.TempDir()
	repo, _, _ := newRepoWithVault(t, root, "a", "")

	shared := filepath.Join(repo, ".git", "paivot", "nd-vault")
	if _, err := store.Init(shared, "S", "tester"); err != nil {
		t.Fatal(err)
	}
	r, err := DiscoverRepo(shared)
	if err != nil {
		t.Fatalf("discover from shared vault: %v", err)
	}
	want := filepath.Join(repo, ".git")
	if r.GitDir != want {
		t.Errorf("GitDir = %s, want %s", r.GitDir, want)
	}

	if _, err := Snapshot(shared, "nd/shared-backlog", "shared snap", false); err != nil {
		t.Fatalf("snapshot shared vault: %v", err)
	}
}
