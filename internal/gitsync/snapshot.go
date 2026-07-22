package gitsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// snapshotExcludes are vault paths that never belong in a backlog snapshot:
// runtime state, locks, trash, loop state, and local archives. Everything
// else in the vault (config, issues, knowledge notes) is captured.
var snapshotExcludes = []string{
	":(exclude).trash",
	":(exclude).guard",
	":(exclude).vlt.lock",
	":(exclude).nd-shared.yaml",
	":(exclude).piv-loop-state.json",
	":(exclude).piv-loop-snapshot.json",
	":(exclude).dispatcher-state.json",
	":(exclude)archive-*",
	":(exclude)**/.*.tmp-*",
	":(exclude).*.tmp-*",
}

// SnapshotResult reports what a snapshot did.
type SnapshotResult struct {
	Commit  string // tip of the branch after the snapshot
	Created bool   // false when the tree was unchanged and no commit was made
}

// Snapshot commits the current vault contents to the backlog branch. It is
// idempotent: when the vault tree matches the branch tip, no commit is made.
//
// The caller must hold the vault's exclusive lock so the captured tree is a
// consistent cut (multi-file operations like dep add touch two files).
func Snapshot(vaultDir, branch, message string, force bool) (*SnapshotResult, error) {
	if _, err := os.Stat(filepath.Join(vaultDir, ".nd.yaml")); err != nil {
		return nil, fmt.Errorf("vault %s has no .nd.yaml; refusing to snapshot (wiped or uninitialized vault?)", vaultDir)
	}

	r, err := DiscoverRepo(vaultDir)
	if err != nil {
		return nil, err
	}

	tree, err := r.writeTreeFromDir(vaultDir)
	if err != nil {
		return nil, err
	}

	tip, err := r.BranchTip(branch)
	if err != nil {
		return nil, err
	}

	if tip != "" {
		tipTree, err := r.treeOf(tip)
		if err != nil {
			return nil, err
		}
		if tipTree == tree {
			return &SnapshotResult{Commit: tip, Created: false}, nil
		}
		if !force {
			if err := r.guardForeignBranch(branch, tip); err != nil {
				return nil, err
			}
			if err := r.guardMassDelete(tipTree, tree); err != nil {
				return nil, err
			}
		}
	}

	commit, err := r.commitTree(tree, message, tipParents(tip))
	if err != nil {
		return nil, err
	}
	if err := r.updateBranch(branch, commit, tip); err != nil {
		return nil, err
	}
	return &SnapshotResult{Commit: commit, Created: true}, nil
}

func tipParents(tip string) []string {
	if tip == "" {
		return nil
	}
	return []string{tip}
}

// guardForeignBranch refuses to commit vault snapshots onto a pre-existing
// branch that was never an nd backlog branch (its tip has no .nd.yaml). A
// user's unrelated branch that happens to share the configured name must not
// be hijacked; point sync.branch elsewhere or force intentionally.
func (r *Repo) guardForeignBranch(branch, tip string) error {
	_, err := r.git(nil, "cat-file", "-e", tip+":.nd.yaml")
	if err != nil {
		return fmt.Errorf("branch %q exists but is not an nd backlog branch (no .nd.yaml at its tip); "+
			"set `nd config set sync.branch <name>` to use a different branch, or `nd sync --force` to take it over", branch)
	}
	return nil
}

// guardMassDelete refuses to record a snapshot that silently drops the whole
// backlog: an empty issues/ over a branch that had issues is far more likely
// a wiped vault (git clean, deleted checkout) than an intentional purge.
// `nd sync --restore` recovers the vault; `nd sync --force` overrides.
func (r *Repo) guardMassDelete(oldTree, newTree string) error {
	oldCount, err := r.countIssues(oldTree)
	if err != nil {
		return err
	}
	newCount, err := r.countIssues(newTree)
	if err != nil {
		return err
	}
	if newCount == 0 && oldCount > 0 {
		return fmt.Errorf("refusing snapshot: vault has 0 issues but backlog branch has %d; "+
			"run `nd sync --restore` to recover the backlog, or `nd sync --force` if the deletion is intentional", oldCount)
	}
	return nil
}

func (r *Repo) countIssues(tree string) (int, error) {
	out, err := r.git(nil, "ls-tree", "-r", "--name-only", tree, "--", "issues/")
	if err != nil {
		return 0, err
	}
	if out == "" {
		return 0, nil
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, ".md") {
			n++
		}
	}
	return n, nil
}

// writeTreeFromDir builds a git tree object from the contents of dir using a
// throwaway index. The repository's real index and working tree are untouched.
func (r *Repo) writeTreeFromDir(dir string) (string, error) {
	idx, err := os.CreateTemp("", "nd-sync-index-*")
	if err != nil {
		return "", err
	}
	idxPath := idx.Name()
	idx.Close()
	os.Remove(idxPath) // git add recreates it; an empty existing file confuses git
	defer os.Remove(idxPath)

	env := []string{"GIT_INDEX_FILE=" + idxPath}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	addArgs := append([]string{"--work-tree", absDir, "add", "-f", "-A", "--", "."}, snapshotExcludes...)
	if _, err := r.gitInDir(absDir, env, addArgs...); err != nil {
		return "", err
	}

	return r.git(env, "write-tree")
}

// gitInDir runs git with the working directory set, needed for pathspecs that
// are relative to the work tree root.
func (r *Repo) gitInDir(dir string, extraEnv []string, args ...string) (string, error) {
	return r.run(dir, nil, extraEnv, args...)
}

func (r *Repo) commitTree(tree, message string, parents []string) (string, error) {
	args := []string{"commit-tree", tree, "-m", message}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	return r.git(r.identityEnv(), args...)
}

// updateBranch atomically moves the branch ref from oldTip to commit.
// An empty oldTip asserts the branch does not exist yet.
func (r *Repo) updateBranch(branch, commit, oldTip string) error {
	_, err := r.git(nil, "update-ref", RefName(branch), commit, oldTip)
	return err
}
