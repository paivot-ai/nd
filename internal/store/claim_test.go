package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paivot-ai/nd/internal/model"
)

func TestClaimIssue(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "T", "tester")
	if err != nil {
		t.Fatal(err)
	}

	issue, err := s.CreateIssue("Claimable", "d", "task", 2, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := s.ClaimIssue(issue.ID, "agent-1", false)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.Assignee != "agent-1" || claimed.Status != model.StatusInProgress {
		t.Fatalf("claim result: assignee=%q status=%s", claimed.Assignee, claimed.Status)
	}

	// Second agent must be refused.
	if _, err := s.ClaimIssue(issue.ID, "agent-2", false); err == nil {
		t.Fatal("expected second claim to fail")
	}

	// Same agent re-claim is idempotent.
	if _, err := s.ClaimIssue(issue.ID, "agent-1", false); err != nil {
		t.Fatalf("idempotent re-claim: %v", err)
	}

	// Force steals.
	stolen, err := s.ClaimIssue(issue.ID, "agent-2", true)
	if err != nil {
		t.Fatalf("forced claim: %v", err)
	}
	if stolen.Assignee != "agent-2" {
		t.Fatalf("forced claim assignee = %q", stolen.Assignee)
	}
	if !strings.Contains(stolen.Body, "stolen from agent-1") {
		t.Error("steal not recorded in history")
	}
}

func TestClaimBlockedIssueRefused(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "T", "tester")
	if err != nil {
		t.Fatal(err)
	}
	blocker, _ := s.CreateIssue("Blocker", "d", "task", 2, "", nil, "")
	blocked, _ := s.CreateIssue("Blocked", "d", "task", 2, "", nil, "")
	if err := s.AddDependency(blocked.ID, blocker.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ClaimIssue(blocked.ID, "agent-1", false); err == nil {
		t.Fatal("expected claim of blocked issue to fail")
	}
	if _, err := s.ClaimIssue(blocked.ID, "agent-1", true); err != nil {
		t.Fatalf("forced claim of blocked issue: %v", err)
	}

	// After the blocker closes, claiming works normally.
	blocked2, _ := s.CreateIssue("Blocked2", "d", "task", 2, "", nil, "")
	if err := s.AddDependency(blocked2.ID, blocker.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseIssue(blocker.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimIssue(blocked2.ID, "agent-1", false); err != nil {
		t.Fatalf("claim after blocker closed: %v", err)
	}
}

func TestReleaseIssue(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "T", "tester")
	if err != nil {
		t.Fatal(err)
	}
	issue, _ := s.CreateIssue("Releasable", "d", "task", 2, "", nil, "")
	if _, err := s.ClaimIssue(issue.ID, "agent-1", false); err != nil {
		t.Fatal(err)
	}

	// Another agent cannot release without force.
	if _, err := s.ReleaseIssue(issue.ID, "agent-2", false); err == nil {
		t.Fatal("expected foreign release to fail")
	}

	released, err := s.ReleaseIssue(issue.ID, "agent-1", false)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.Assignee != "" || released.Status != model.StatusOpen {
		t.Fatalf("release result: assignee=%q status=%s", released.Assignee, released.Status)
	}
}

func TestClaimClosedIssueRefused(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "T", "tester")
	if err != nil {
		t.Fatal(err)
	}
	issue, _ := s.CreateIssue("Done", "d", "task", 2, "", nil, "")
	if err := s.CloseIssue(issue.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimIssue(issue.ID, "agent-1", false); err == nil {
		t.Fatal("expected claim of closed issue to fail")
	}
}

func TestGitignoreReconcileOnModeSwitch(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "T", "tester")
	if err != nil {
		t.Fatal(err)
	}

	read := func() string {
		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	if !strings.Contains(read(), "issues/") {
		t.Fatal("default mode should ignore issues/")
	}

	// Switching to tracked mode must REMOVE the ignore entries, not just
	// stop adding them.
	if err := s.SetConfigValue("track_issues", "true"); err != nil {
		t.Fatalf("set track_issues: %v", err)
	}
	content := read()
	for _, stale := range []string{"\nissues/\n", "\n.nd.yaml\n"} {
		if strings.Contains(content, stale) {
			t.Errorf("tracked mode .gitignore still contains %q:\n%s", strings.TrimSpace(stale), content)
		}
	}
	if !strings.Contains(content, ".vlt.lock") {
		t.Error("runtime entries must survive the mode switch")
	}

	// And back.
	if err := s.SetConfigValue("track_issues", "false"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(), "issues/") {
		t.Error("untracked mode should re-add issues/ ignore")
	}
}

func TestSyncConfigKeys(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "T", "tester")
	if err != nil {
		t.Fatal(err)
	}

	if s.SyncBranch() != "nd/backlog" || s.SyncRemote() != "origin" || !s.SyncAutoEnabled() {
		t.Fatal("unexpected sync defaults")
	}

	if err := s.SetConfigValue("sync.branch", "backlog/main"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfigValue("sync.auto", "off"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfigValue("sync.branch", "bad name"); err == nil {
		t.Error("expected invalid branch name to be rejected")
	}

	// Reload from disk.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.SyncBranch() != "backlog/main" {
		t.Errorf("sync.branch = %q after reload", s2.SyncBranch())
	}
	if s2.SyncAutoEnabled() {
		t.Error("sync.auto=off not persisted")
	}
}
