package gitsync

import (
	"fmt"
	"strings"
)

// SyncOptions controls a Sync run.
type SyncOptions struct {
	Remote  string // remote name; empty uses DefaultRemote
	Branch  string // backlog branch; empty uses DefaultBranch
	NoPush  bool   // skip pushing even when a remote exists
	Force   bool   // bypass the mass-delete snapshot guard
	Message string // snapshot commit message; empty uses "nd sync"
}

// SyncResult describes what a Sync run did.
type SyncResult struct {
	Branch       string
	Remote       string
	Snapshotted  bool     // a new local snapshot commit was created
	Pulled       bool     // remote changes were materialized into the vault
	Merged       bool     // a true three-way merge was required
	Pushed       bool     // local branch was pushed
	RemoteAbsent bool     // no usable remote: local-only sync
	Commit       string   // branch tip after the run
	Notes        []string // merge resolution notes
}

// Sync snapshots the vault, reconciles with the remote backlog branch, and
// pushes. Safe to run at any time; the vault's pre-sync state is always
// committed first, so no local state can be lost by a merge.
//
// The caller must hold the vault's exclusive lock for the entire run.
func Sync(vaultDir string, opts SyncOptions) (*SyncResult, error) {
	if opts.Branch == "" {
		opts.Branch = DefaultBranch
	}
	if opts.Remote == "" {
		opts.Remote = DefaultRemote
	}
	if opts.Message == "" {
		opts.Message = "nd sync"
	}

	res := &SyncResult{Branch: opts.Branch, Remote: opts.Remote}

	snap, err := Snapshot(vaultDir, opts.Branch, opts.Message, opts.Force)
	if err != nil {
		return nil, err
	}
	res.Snapshotted = snap.Created
	res.Commit = snap.Commit

	r, err := DiscoverRepo(vaultDir)
	if err != nil {
		return nil, err
	}
	if !r.HasRemote(opts.Remote) {
		res.RemoteAbsent = true
		return res, nil
	}

	const maxAttempts = 3
	for attempt := 1; ; attempt++ {
		remoteTip, err := r.fetchBranch(opts.Remote, opts.Branch)
		if err != nil {
			return nil, err
		}
		localTip, err := r.BranchTip(opts.Branch)
		if err != nil {
			return nil, err
		}

		if remoteTip == "" {
			// Remote branch does not exist yet: publish ours.
			if opts.NoPush {
				return res, nil
			}
			if err := r.push(opts.Remote, opts.Branch); err != nil {
				return nil, err
			}
			res.Pushed = true
			return res, nil
		}

		if remoteTip == localTip {
			res.Commit = localTip
			return res, nil
		}

		mergeBase, _ := r.git(nil, "merge-base", localTip, remoteTip)

		switch mergeBase {
		case remoteTip: // local is ahead
			if opts.NoPush {
				return res, nil
			}
			err = r.push(opts.Remote, opts.Branch)
			if err == nil {
				res.Pushed = true
				return res, nil
			}
			if attempt >= maxAttempts {
				return nil, fmt.Errorf("push rejected after %d attempts: %w", maxAttempts, err)
			}
			continue // remote moved underneath us: refetch and reconcile

		case localTip: // remote is ahead: fast-forward and materialize
			if err := r.updateBranch(opts.Branch, remoteTip, localTip); err != nil {
				return nil, err
			}
			if err := Materialize(vaultDir, remoteTip); err != nil {
				return nil, err
			}
			res.Pulled = true
			res.Commit = remoteTip
			return res, nil

		default: // diverged: three-way merge
			base := mergeBase
			if base == "" {
				// Unrelated histories (both machines created the branch
				// independently): merge against the empty tree.
				empty, eerr := r.emptyTree()
				if eerr != nil {
					return nil, eerr
				}
				baseCommit, cerr := r.commitTree(empty, "nd sync: synthetic empty base", nil)
				if cerr != nil {
					return nil, cerr
				}
				base = baseCommit
			}
			tree, notes, err := r.MergeCommits(base, localTip, remoteTip)
			if err != nil {
				return nil, err
			}
			mergeMsg := fmt.Sprintf("nd sync: merge %s and %s", short(localTip), short(remoteTip))
			mergeCommit, err := r.commitTree(tree, mergeMsg, []string{localTip, remoteTip})
			if err != nil {
				return nil, err
			}
			if err := r.updateBranch(opts.Branch, mergeCommit, localTip); err != nil {
				return nil, err
			}
			if err := Materialize(vaultDir, mergeCommit); err != nil {
				return nil, err
			}
			res.Pulled = true
			res.Merged = true
			res.Commit = mergeCommit
			res.Notes = append(res.Notes, notes...)

			if opts.NoPush {
				return res, nil
			}
			err = r.push(opts.Remote, opts.Branch)
			if err == nil {
				res.Pushed = true
				return res, nil
			}
			if attempt >= maxAttempts {
				return nil, fmt.Errorf("push rejected after %d attempts: %w", maxAttempts, err)
			}
		}
	}
}

// Restore materializes the backlog branch into the vault directory,
// recovering a wiped or freshly cloned vault. When the local branch is absent
// it is fetched from the remote first.
func Restore(vaultDir string, opts SyncOptions) (string, error) {
	if opts.Branch == "" {
		opts.Branch = DefaultBranch
	}
	if opts.Remote == "" {
		opts.Remote = DefaultRemote
	}

	r, err := DiscoverRepo(vaultDir)
	if err != nil {
		return "", err
	}
	tip, err := r.BranchTip(opts.Branch)
	if err != nil {
		return "", err
	}
	if tip == "" && r.HasRemote(opts.Remote) {
		remoteTip, ferr := r.fetchBranch(opts.Remote, opts.Branch)
		if ferr != nil {
			return "", ferr
		}
		if remoteTip != "" {
			if err := r.updateBranch(opts.Branch, remoteTip, ""); err != nil {
				return "", err
			}
			tip = remoteTip
		}
	}
	if tip == "" {
		return "", fmt.Errorf("no backlog branch %q locally or on %q; nothing to restore", opts.Branch, opts.Remote)
	}
	if err := Materialize(vaultDir, tip); err != nil {
		return "", err
	}
	return tip, nil
}

// Status reports the sync position of the vault without changing anything.
type StatusResult struct {
	Branch       string
	BranchExists bool
	Commit       string
	Dirty        bool // vault tree differs from the branch tip
	RemoteAbsent bool
	Ahead        int // commits on local branch not on remote
	Behind       int // commits on remote branch not on local
}

// Status compares the vault, the local backlog branch, and the remote branch.
// It performs a fetch when a remote is configured but never writes refs,
// vault files, or snapshots.
func Status(vaultDir string, opts SyncOptions) (*StatusResult, error) {
	if opts.Branch == "" {
		opts.Branch = DefaultBranch
	}
	if opts.Remote == "" {
		opts.Remote = DefaultRemote
	}
	r, err := DiscoverRepo(vaultDir)
	if err != nil {
		return nil, err
	}
	res := &StatusResult{Branch: opts.Branch}

	tip, err := r.BranchTip(opts.Branch)
	if err != nil {
		return nil, err
	}
	res.Commit = tip
	res.BranchExists = tip != ""

	tree, err := r.writeTreeFromDir(vaultDir)
	if err != nil {
		return nil, err
	}
	if tip == "" {
		res.Dirty = true
	} else {
		tipTree, err := r.treeOf(tip)
		if err != nil {
			return nil, err
		}
		res.Dirty = tree != tipTree
	}

	if !r.HasRemote(opts.Remote) {
		res.RemoteAbsent = true
		return res, nil
	}
	remoteTip, err := r.fetchBranch(opts.Remote, opts.Branch)
	if err != nil || remoteTip == "" {
		res.RemoteAbsent = err != nil
		if tip != "" {
			res.Ahead, _ = r.countRange(remoteTip, tip)
		}
		return res, nil
	}
	if tip != "" {
		res.Ahead, _ = r.countRange(remoteTip, tip)
		res.Behind, _ = r.countRange(tip, remoteTip)
	} else {
		res.Behind, _ = r.countRange("", remoteTip)
	}
	return res, nil
}

// fetchBranch fetches the remote backlog branch and returns its tip, or ""
// when the remote branch does not exist.
func (r *Repo) fetchBranch(remote, branch string) (string, error) {
	_, err := r.git(nil, "fetch", "--quiet", remote, RefName(branch))
	if err != nil {
		if strings.Contains(err.Error(), "couldn't find remote ref") {
			return "", nil
		}
		return "", err
	}
	return r.git(nil, "rev-parse", "FETCH_HEAD")
}

func (r *Repo) push(remote, branch string) error {
	_, err := r.git(nil, "push", "--quiet", remote, RefName(branch)+":"+RefName(branch))
	return err
}

// countRange counts commits reachable from tip but not from exclude.
func (r *Repo) countRange(exclude, tip string) (int, error) {
	args := []string{"rev-list", "--count", tip}
	if exclude != "" {
		args = []string{"rev-list", "--count", exclude + ".." + tip}
	}
	out, err := r.git(nil, args...)
	if err != nil {
		return 0, err
	}
	n := 0
	fmt.Sscanf(out, "%d", &n)
	return n, nil
}

func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}
