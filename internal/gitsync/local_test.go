package gitsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalSyncAndStatusNeverContactRemote(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	gitCmd(t, root, "init", "--bare", "-q", remote)
	repo, vault, s := newRepoWithVault(t, root, "repo", remote)
	defer s.Close()
	if _, err := s.CreateIssue("Local snapshot", "body", "task", 2, "", nil, ""); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(root, "git.trace")
	t.Setenv("GIT_TRACE", trace)
	res, err := Sync(vault, SyncOptions{Local: true})
	if err != nil || !res.LocalOnly || !res.Snapshotted || res.Pushed || res.Pulled {
		t.Fatalf("local sync: %+v, %v", res, err)
	}
	again, err := Sync(vault, SyncOptions{Local: true})
	if err != nil || again.Snapshotted || again.Commit != res.Commit {
		t.Fatalf("idle local sync: %+v, %v", again, err)
	}
	st, err := Status(vault, SyncOptions{Local: true})
	if err != nil || st.Dirty || !st.RemoteUnchecked || !st.BranchExists {
		t.Fatalf("local status: %+v, %v", st, err)
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"Ahead", "Behind", "RemoteAbsent"} {
		if strings.Contains(string(data), field) {
			t.Fatalf("unchecked JSON contains %s: %s", field, data)
		}
	}
	calls, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{" remote ", " fetch ", " push "} {
		if strings.Contains(string(calls), command) {
			t.Fatalf("local command reached %q: %s", command, calls)
		}
	}
	if got := gitCmd(t, repo, "remote", "get-url", "origin"); got != remote {
		t.Fatalf("remote changed: %s", got)
	}
	online, err := Sync(vault, SyncOptions{})
	if err != nil || !online.Pushed {
		t.Fatalf("explicit sync: %+v, %v", online, err)
	}
}

func TestLocalRestoreAndSnapshotFailures(t *testing.T) {
	root := t.TempDir()
	_, vault, s := newRepoWithVault(t, root, "repo", filepath.Join(root, "missing.git"))
	if _, err := s.CreateIssue("Restore me", "preserve body", "task", 2, "", nil, ""); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(vault, SyncOptions{Local: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	saved := vault + ".saved"
	if err := os.Rename(vault, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(vault, SyncOptions{Local: true}); err == nil {
		t.Fatal("missing vault was accepted")
	}
	commit, err := Restore(vault, SyncOptions{Local: true})
	if err != nil || commit != res.Commit {
		t.Fatalf("restore: %s, %v", commit, err)
	}
	files, err := os.ReadDir(filepath.Join(saved, "issues"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		before, err := os.ReadFile(filepath.Join(saved, "issues", file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(vault, "issues", file.Name()))
		if err != nil || string(before) != string(after) {
			t.Fatalf("restored issue differs: %s, %v", file.Name(), err)
		}
	}
	if _, err := Restore(vault, SyncOptions{Local: true, Branch: "missing"}); err == nil {
		t.Fatal("missing local branch was accepted")
	}
	if err := os.Rename(filepath.Join(vault, "issues"), filepath.Join(root, "saved-issues")); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(vault, SyncOptions{Local: true}); err == nil {
		t.Fatal("mass-delete guard was bypassed")
	}
}
