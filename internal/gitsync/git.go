// Package gitsync persists the nd vault to a dedicated git branch.
//
// The live vault is always a single physical directory (branch-independent,
// shared across worktrees). gitsync gives that directory durability and
// transport: every mutation can be snapshotted as a commit on a backlog
// branch (default nd/backlog), and `nd sync` fetches, merges, and pushes
// that branch across clones. Code branches never carry backlog state, so
// the backlog cannot diverge with the level of parallelization.
//
// All operations use git plumbing (temp index, write-tree, commit-tree,
// update-ref) and never touch the repository's working tree or index.
package gitsync

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultBranch is the backlog branch used when the vault config does not
// override it.
const DefaultBranch = "nd/backlog"

// DefaultRemote is the remote used when the vault config does not override it.
const DefaultRemote = "origin"

// Repo locates the git object database that backs a vault.
type Repo struct {
	// GitDir is the git common dir: objects and refs live here. Using the
	// common dir makes every worktree of the repo see the same backlog branch.
	GitDir string
}

// DiscoverRepo finds the repository that owns the vault directory. It works
// for vaults inside a worktree (.vault/) and for shared vaults that live
// under the git common dir itself (<repo>/.git/paivot/nd-vault).
func DiscoverRepo(vaultDir string) (*Repo, error) {
	abs, err := filepath.Abs(vaultDir)
	if err != nil {
		return nil, err
	}
	dir := abs
	for {
		gitPath := filepath.Join(dir, ".git")
		if info, statErr := os.Stat(gitPath); statErr == nil {
			commonDir, cerr := resolveCommonDir(dir, gitPath, info.IsDir())
			if cerr != nil {
				return nil, cerr
			}
			return &Repo{GitDir: commonDir}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("vault %s is not inside a git repository", vaultDir)
		}
		dir = parent
	}
}

// resolveCommonDir resolves .git (dir or worktree pointer file) to the git
// common dir.
func resolveCommonDir(repoRoot, gitPath string, isDir bool) (string, error) {
	gitDir := gitPath
	if !isDir {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(string(data))
		const prefix = "gitdir:"
		if !strings.HasPrefix(line, prefix) {
			return "", fmt.Errorf("%s does not contain a gitdir pointer", gitPath)
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(repoRoot, gitDir)
		}
		gitDir = filepath.Clean(gitDir)
	}
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimSpace(string(data))
		if common != "" {
			if !filepath.IsAbs(common) {
				common = filepath.Join(gitDir, common)
			}
			return filepath.Clean(common), nil
		}
	}
	return filepath.Clean(gitDir), nil
}

// git runs a git command against the repo's object database. The caller's
// GIT_* environment is scrubbed so ambient state (GIT_DIR, GIT_INDEX_FILE,
// GIT_WORK_TREE set by other tooling) cannot redirect plumbing operations.
func (r *Repo) git(extraEnv []string, args ...string) (string, error) {
	return r.run("", nil, extraEnv, args...)
}

// gitStdin is git() with a stdin payload.
func (r *Repo) gitStdin(stdin []byte, extraEnv []string, args ...string) (string, error) {
	return r.run("", stdin, extraEnv, args...)
}

// run is the single exec path for all git invocations. dir sets the process
// working directory (needed for work-tree-relative pathspecs); stdin and
// extraEnv are optional.
func (r *Repo) run(dir string, stdin []byte, extraEnv []string, args ...string) (string, error) {
	full := append([]string{"--git-dir", r.GitDir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(scrubGitEnv(os.Environ()), extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "),
			strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func scrubGitEnv(env []string) []string {
	out := env[:0:0]
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_DIR=") ||
			strings.HasPrefix(e, "GIT_WORK_TREE=") ||
			strings.HasPrefix(e, "GIT_INDEX_FILE=") ||
			strings.HasPrefix(e, "GIT_OBJECT_DIRECTORY=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// RefName returns the fully qualified ref for a backlog branch name.
func RefName(branch string) string { return "refs/heads/" + branch }

// BranchTip returns the commit hash of the backlog branch, or "" when the
// branch does not exist.
func (r *Repo) BranchTip(branch string) (string, error) {
	out, err := r.git(nil, "rev-parse", "--verify", "--quiet", RefName(branch))
	if err != nil {
		// rev-parse --verify --quiet exits 1 when the ref is missing.
		return "", nil
	}
	return out, nil
}

// treeOf returns the tree hash of a commit.
func (r *Repo) treeOf(commit string) (string, error) {
	return r.git(nil, "rev-parse", commit+"^{tree}")
}

// emptyTree writes (or finds) the empty tree object and returns its hash.
// Computed rather than hard-coded so SHA-256 repositories work.
func (r *Repo) emptyTree() (string, error) {
	return r.gitStdin(nil, nil, "mktree")
}

// identityEnv returns author/committer env vars for snapshot commits, using
// the repo's configured identity with an "nd" fallback so commits never fail
// on machines without git identity configured (CI, fresh agents).
func (r *Repo) identityEnv() []string {
	name, _ := r.git(nil, "config", "user.name")
	email, _ := r.git(nil, "config", "user.email")
	if name == "" {
		name = "nd"
	}
	if email == "" {
		email = "nd@localhost"
	}
	return []string{
		"GIT_AUTHOR_NAME=" + name,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_COMMITTER_NAME=" + name,
		"GIT_COMMITTER_EMAIL=" + email,
	}
}

// HasRemote reports whether the named remote is configured.
func (r *Repo) HasRemote(remote string) bool {
	_, err := r.git(nil, "remote", "get-url", remote)
	return err == nil
}
