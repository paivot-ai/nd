package gitsync

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Materialize writes the contents of a backlog commit into the vault
// directory. Existing files are replaced atomically (temp file + rename).
// Issue files present in the vault but absent from the commit are moved to
// .trash/ rather than deleted. Non-issue files are never pruned.
//
// The caller must hold the vault's exclusive lock.
func Materialize(vaultDir, commit string) error {
	r, err := DiscoverRepo(vaultDir)
	if err != nil {
		return err
	}
	paths, err := r.listTreeFiles(commit)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("commit %s has an empty tree; refusing to materialize", commit)
	}

	if err := os.MkdirAll(filepath.Join(vaultDir, "issues"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(vaultDir, ".trash"), 0o755); err != nil {
		return err
	}

	blobs, err := r.readBlobs(commit, paths)
	if err != nil {
		return err
	}
	inTree := make(map[string]bool, len(paths))
	for _, p := range paths {
		inTree[p] = true
		dst := filepath.Join(vaultDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := atomicWrite(dst, blobs[p]); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}

	// Prune vault issue files that the commit does not carry (soft: to trash).
	entries, err := os.ReadDir(filepath.Join(vaultDir, "issues"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := "issues/" + e.Name()
		if inTree[rel] {
			continue
		}
		src := filepath.Join(vaultDir, "issues", e.Name())
		dst := filepath.Join(vaultDir, ".trash", e.Name())
		if _, statErr := os.Stat(dst); statErr == nil {
			dst = filepath.Join(vaultDir, ".trash",
				fmt.Sprintf("%s.%d.md", strings.TrimSuffix(e.Name(), ".md"), time.Now().UnixNano()))
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("trash stale issue %s: %w", e.Name(), err)
		}
	}
	return nil
}

// listTreeFiles returns the slash-separated paths of all blobs in a commit.
func (r *Repo) listTreeFiles(commit string) ([]string, error) {
	out, err := r.git(nil, "ls-tree", "-r", "-z", "--name-only", commit)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// readBlobs fetches the contents of many paths from a commit with a single
// `git cat-file --batch` process.
func (r *Repo) readBlobs(commit string, paths []string) (map[string][]byte, error) {
	var stdin bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&stdin, "%s:%s\n", commit, p)
	}

	cmd := exec.Command("git", "--git-dir", r.GitDir, "cat-file", "--batch")
	cmd.Env = scrubGitEnv(os.Environ())
	cmd.Stdin = &stdin
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	result := make(map[string][]byte, len(paths))
	reader := bufio.NewReader(stdout)
	for _, p := range paths {
		header, err := reader.ReadString('\n')
		if err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("cat-file header for %s: %w", p, err)
		}
		fields := strings.Fields(strings.TrimSpace(header))
		if len(fields) == 2 && fields[1] == "missing" {
			cmd.Wait()
			return nil, fmt.Errorf("object missing for %s", p)
		}
		if len(fields) != 3 {
			cmd.Wait()
			return nil, fmt.Errorf("unexpected cat-file header %q for %s", strings.TrimSpace(header), p)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("bad size in cat-file header %q: %w", header, err)
		}
		content := make([]byte, size)
		if _, err := io.ReadFull(reader, content); err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("cat-file content for %s: %w", p, err)
		}
		// Trailing newline after each object.
		if _, err := reader.Discard(1); err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("cat-file separator for %s: %w", p, err)
		}
		result[p] = content
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("cat-file --batch: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return result, nil
}

// atomicWrite writes content via temp file + rename in the destination
// directory, matching vlt's crash-safe write pattern.
func atomicWrite(dst string, content []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, dst)
}
