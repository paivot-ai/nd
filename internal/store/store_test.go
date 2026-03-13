package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RamXX/nd/internal/model"
)

func gitignoreLineSet(content string) map[string]bool {
	lines := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		lines[strings.TrimSpace(line)] = true
	}
	return lines
}

func TestInit_CreatesGitignore(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore missing: %v", err)
	}

	content := string(data)
	lines := gitignoreLineSet(content)
	for _, entry := range gitignoreEntries(false) {
		if !lines[entry] {
			t.Errorf(".gitignore missing entry %q", entry)
		}
	}
}

func TestEnsureGitignore_Idempotent(t *testing.T) {
	dir := t.TempDir()

	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Read original content.
	original, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	// Call EnsureGitignore again.
	if err := s.EnsureGitignore(); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore after: %v", err)
	}

	if string(original) != string(after) {
		t.Errorf("EnsureGitignore should be idempotent.\nBefore:\n%s\nAfter:\n%s", original, after)
	}
}

func TestEnsureGitignore_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()

	// Create a partial .gitignore with only some entries.
	partial := "# custom user entry\n*.log\n.nd.yaml\n"
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(partial), 0o644); err != nil {
		t.Fatalf("write partial .gitignore: %v", err)
	}

	// Create minimal vault structure so we can make a Store.
	for _, sub := range []string{"issues", ".trash"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// EnsureGitignore should add missing entries.
	if err := s.EnsureGitignore(); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	content := string(data)
	lines := gitignoreLineSet(content)
	// Original entries must still be there.
	if !lines["# custom user entry"] {
		t.Error("original custom entry should be preserved")
	}
	if !lines["*.log"] {
		t.Error("original *.log entry should be preserved")
	}

	// All required entries must be present.
	for _, entry := range gitignoreEntries(false) {
		if !lines[entry] {
			t.Errorf("missing entry %q after EnsureGitignore", entry)
		}
	}

	// .nd.yaml should not be duplicated (it was in the partial).
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == ".nd.yaml" {
			count++
		}
	}
	if count != 1 {
		t.Errorf(".nd.yaml ignore entry should appear exactly once, got %d", count)
	}
}

func TestInit_TrackIssuesKeepsIssuesAndConfigTracked(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(dir, "TST", "tester", InitOptions{TrackIssues: true})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	content := string(data)
	lines := gitignoreLineSet(content)
	for _, entry := range gitignoreEntries(true) {
		if !lines[entry] {
			t.Errorf(".gitignore missing tracked-mode entry %q", entry)
		}
	}
	if lines["issues/"] {
		t.Error("tracked mode should not ignore issues/")
	}
	if lines[".nd.yaml"] {
		t.Error("tracked mode should not ignore .nd.yaml")
	}

	cfg, err := os.ReadFile(filepath.Join(dir, ".nd.yaml"))
	if err != nil {
		t.Fatalf("read .nd.yaml: %v", err)
	}
	if !strings.Contains(string(cfg), "track_issues: true") {
		t.Error("tracked mode should persist track_issues: true in .nd.yaml")
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	after, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore after open: %v", err)
	}
	afterLines := gitignoreLineSet(string(after))
	if afterLines["issues/"] {
		t.Error("Open should not re-add issues/ ignore entry in tracked mode")
	}
	if afterLines[".nd.yaml"] {
		t.Error("Open should not re-add .nd.yaml ignore entry in tracked mode")
	}
}

func TestInitAndOpen(t *testing.T) {
	dir := t.TempDir()

	// Init.
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if s.Prefix() != "TST" {
		t.Errorf("prefix = %q, want TST", s.Prefix())
	}

	// Verify .nd.yaml exists.
	if _, err := os.Stat(dir + "/.nd.yaml"); err != nil {
		t.Fatalf(".nd.yaml missing: %v", err)
	}
	// Verify issues/ dir exists.
	if _, err := os.Stat(dir + "/issues"); err != nil {
		t.Fatalf("issues/ missing: %v", err)
	}

	// Reopen.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s2.Prefix() != "TST" {
		t.Errorf("reopened prefix = %q, want TST", s2.Prefix())
	}
}

func TestCreateAndReadIssue(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, err := s.CreateIssue("Fix the login bug", "Users can't login when password has special chars", "bug", 1, "alice", []string{"auth"}, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if issue.ID == "" {
		t.Fatal("issue ID is empty")
	}
	if issue.Title != "Fix the login bug" {
		t.Errorf("title = %q", issue.Title)
	}
	if issue.Status != "open" {
		t.Errorf("status = %q", issue.Status)
	}
	if issue.Priority != 1 {
		t.Errorf("priority = %d", issue.Priority)
	}

	// Read back.
	got, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("ReadIssue: %v", err)
	}
	if got.ID != issue.ID {
		t.Errorf("read ID = %q, want %q", got.ID, issue.ID)
	}
	if got.Title != issue.Title {
		t.Errorf("read title = %q, want %q", got.Title, issue.Title)
	}
	if got.Assignee != "alice" {
		t.Errorf("assignee = %q, want alice", got.Assignee)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "auth" {
		t.Errorf("labels = %v, want [auth]", got.Labels)
	}
	if got.ContentHash == "" {
		t.Error("content hash is empty")
	}

	// Verify file on disk.
	if _, err := os.Stat(dir + "/issues/" + issue.ID + ".md"); err != nil {
		t.Errorf("issue file missing: %v", err)
	}
}

func TestListIssues(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	_, err = s.CreateIssue("Issue B", "", "bug", 0, "bob", nil, "")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	// List all.
	all, err := s.ListIssues(FilterOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 issues, got %d", len(all))
	}

	// Filter by type.
	bugs, err := s.ListIssues(FilterOptions{Type: "bug"})
	if err != nil {
		t.Fatalf("ListIssues type=bug: %v", err)
	}
	if len(bugs) != 1 {
		t.Errorf("expected 1 bug, got %d", len(bugs))
	}

	// Filter by assignee.
	bobs, err := s.ListIssues(FilterOptions{Assignee: "bob"})
	if err != nil {
		t.Fatalf("ListIssues assignee=bob: %v", err)
	}
	if len(bobs) != 1 {
		t.Errorf("expected 1 assigned to bob, got %d", len(bobs))
	}
}

func TestIssueExists(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if s.IssueExists("TST-xxx") {
		t.Error("nonexistent issue should not exist")
	}

	issue, err := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !s.IssueExists(issue.ID) {
		t.Error("created issue should exist")
	}
}

func TestBuildLinksSection(t *testing.T) {
	tests := []struct {
		name  string
		issue *model.Issue
		want  []string
		empty bool
	}{
		{
			name:  "no relationships",
			issue: &model.Issue{ID: "TST-0001"},
			empty: true,
		},
		{
			name:  "parent only",
			issue: &model.Issue{ID: "TST-0001", Parent: "TST-epic"},
			want:  []string{"- Parent: [[TST-epic]]"},
		},
		{
			name:  "blocks only",
			issue: &model.Issue{ID: "TST-0001", Blocks: []string{"TST-b1", "TST-b2"}},
			want:  []string{"- Blocks: [[TST-b1]], [[TST-b2]]"},
		},
		{
			name:  "blocked_by only",
			issue: &model.Issue{ID: "TST-0001", BlockedBy: []string{"TST-c1"}},
			want:  []string{"- Blocked by: [[TST-c1]]"},
		},
		{
			name:  "related only",
			issue: &model.Issue{ID: "TST-0001", Related: []string{"TST-r1", "TST-r2"}},
			want:  []string{"- Related: [[TST-r1]], [[TST-r2]]"},
		},
		{
			name: "all relationships",
			issue: &model.Issue{
				ID:        "TST-0001",
				Parent:    "TST-epic",
				Blocks:    []string{"TST-b1"},
				BlockedBy: []string{"TST-c1"},
				Related:   []string{"TST-r1"},
			},
			want: []string{
				"- Parent: [[TST-epic]]",
				"- Blocks: [[TST-b1]]",
				"- Blocked by: [[TST-c1]]",
				"- Related: [[TST-r1]]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLinksSection(tt.issue)
			if tt.empty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
}

func TestUpdateLinksSection(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create two issues.
	a, err := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Add dependency: B depends on A.
	if err := s.AddDependency(b.ID, a.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	// Read B and check for wikilinks.
	bRead, err := s.ReadIssue(b.ID)
	if err != nil {
		t.Fatalf("read B: %v", err)
	}
	if !strings.Contains(bRead.Body, "[["+a.ID+"]]") {
		t.Errorf("B body should contain wikilink to A:\n%s", bRead.Body)
	}

	// Read A and check for wikilinks.
	aRead, err := s.ReadIssue(a.ID)
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	if !strings.Contains(aRead.Body, "[["+b.ID+"]]") {
		t.Errorf("A body should contain wikilink to B:\n%s", aRead.Body)
	}

	// Remove dependency and verify active block wikilink moves to was_blocked_by.
	if err := s.RemoveDependency(b.ID, a.ID); err != nil {
		t.Fatalf("remove dep: %v", err)
	}
	bAfter, _ := s.ReadIssue(b.ID)
	if len(bAfter.BlockedBy) != 0 {
		t.Errorf("B should have no active blockers: %v", bAfter.BlockedBy)
	}
	if !contains(bAfter.WasBlockedBy, a.ID) {
		t.Errorf("B.WasBlockedBy should contain A: %v", bAfter.WasBlockedBy)
	}
	// Wikilink still present via was_blocked_by.
	if !strings.Contains(bAfter.Body, "Was blocked by: [["+a.ID+"]]") {
		t.Errorf("B body should contain 'Was blocked by' wikilink to A:\n%s", bAfter.Body)
	}
}

func TestListFilterParent(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	epic, err := s.CreateIssue("Epic", "", "epic", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create epic: %v", err)
	}
	_, err = s.CreateIssue("Child A", "", "task", 2, "", nil, epic.ID)
	if err != nil {
		t.Fatalf("create child A: %v", err)
	}
	_, err = s.CreateIssue("Child B", "", "task", 2, "", nil, epic.ID)
	if err != nil {
		t.Fatalf("create child B: %v", err)
	}
	_, err = s.CreateIssue("Orphan", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create orphan: %v", err)
	}

	// Filter by parent.
	children, err := s.ListIssues(FilterOptions{Parent: epic.ID})
	if err != nil {
		t.Fatalf("list parent=%s: %v", epic.ID, err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}

	// Filter by no-parent.
	orphans, err := s.ListIssues(FilterOptions{NoParent: true})
	if err != nil {
		t.Fatalf("list no-parent: %v", err)
	}
	if len(orphans) != 2 { // epic + orphan
		t.Errorf("expected 2 no-parent issues, got %d", len(orphans))
	}
}

func TestDeleteIssue(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, err := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Add dep: B depends on A.
	if err := s.AddDependency(b.ID, a.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	// Delete A (soft) -- should clean up B's blocked_by.
	modified, err := s.DeleteIssue(a.ID, false)
	if err != nil {
		t.Fatalf("delete A: %v", err)
	}
	if len(modified) == 0 {
		t.Error("expected modified issues from dep cleanup")
	}

	// A should no longer exist.
	if s.IssueExists(a.ID) {
		t.Error("deleted issue should not exist")
	}

	// B should have no blockers.
	bRead, err := s.ReadIssue(b.ID)
	if err != nil {
		t.Fatalf("read B: %v", err)
	}
	if len(bRead.BlockedBy) != 0 {
		t.Errorf("B should have no blockers after A deleted: %v", bRead.BlockedBy)
	}
}

func TestLinksMigration(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create issue and verify it has ## Links section by default.
	issue, err := s.CreateIssue("Test issue", "some desc", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(read.Body, "\n## Links\n") {
		t.Errorf("new issue should have ## Links section:\n%s", read.Body)
	}

	// Call UpdateLinksSection on issue with no relationships (should be idempotent).
	if err := s.UpdateLinksSection(issue.ID); err != nil {
		t.Fatalf("UpdateLinksSection: %v", err)
	}

	// Verify body still has ## Links.
	read2, _ := s.ReadIssue(issue.ID)
	if !strings.Contains(read2.Body, "\n## Links\n") {
		t.Errorf("after UpdateLinksSection, ## Links should still exist:\n%s", read2.Body)
	}
}

func TestConfigCustomStatuses(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initially empty.
	if got := s.CustomStatuses(); len(got) != 0 {
		t.Errorf("expected no custom statuses, got %v", got)
	}

	// Set custom statuses.
	if err := s.SetConfigValue("status.custom", "delivered,accepted,rejected"); err != nil {
		t.Fatalf("SetConfigValue: %v", err)
	}

	custom := s.CustomStatuses()
	if len(custom) != 3 {
		t.Fatalf("expected 3 custom statuses, got %d", len(custom))
	}
	if custom[0] != "delivered" || custom[1] != "accepted" || custom[2] != "rejected" {
		t.Errorf("unexpected custom statuses: %v", custom)
	}
}

func TestConfigSetAndGet(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Set and get.
	if err := s.SetConfigValue("status.custom", "delivered"); err != nil {
		t.Fatalf("set: %v", err)
	}
	val, err := s.GetConfigValue("status.custom")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "delivered" {
		t.Errorf("got %q, want delivered", val)
	}

	// Reopen and verify persistence.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	val2, _ := s2.GetConfigValue("status.custom")
	if val2 != "delivered" {
		t.Errorf("after reopen: got %q, want delivered", val2)
	}
}

func TestConfigValidation(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Reject built-in collision.
	if err := s.SetConfigValue("status.custom", "open"); err == nil {
		t.Error("expected error for built-in collision")
	}

	// Reject bad characters.
	if err := s.SetConfigValue("status.custom", "has spaces"); err == nil {
		t.Error("expected error for invalid name")
	}

	// Reject unknown key.
	if err := s.SetConfigValue("bogus.key", "val"); err == nil {
		t.Error("expected error for unknown key")
	}

	// Set custom, then sequence.
	if err := s.SetConfigValue("status.custom", "delivered,accepted"); err != nil {
		t.Fatalf("set custom: %v", err)
	}

	// Sequence with unknown status should fail.
	if err := s.SetConfigValue("status.sequence", "open,in_progress,unknown,closed"); err == nil {
		t.Error("expected error for unknown status in sequence")
	}

	// Sequence with duplicate should fail.
	if err := s.SetConfigValue("status.sequence", "open,open,closed"); err == nil {
		t.Error("expected error for duplicate in sequence")
	}

	// Valid sequence.
	if err := s.SetConfigValue("status.sequence", "open,in_progress,delivered,accepted,closed"); err != nil {
		t.Fatalf("set sequence: %v", err)
	}

	// FSM without sequence should fail (but we have one now, so test the opposite).
	if err := s.SetConfigValue("status.fsm", "true"); err != nil {
		t.Fatalf("enable fsm: %v", err)
	}

	// Verify FSM is enabled.
	val, _ := s.GetConfigValue("status.fsm")
	if val != "true" {
		t.Errorf("fsm = %q, want true", val)
	}
}

func TestFSMForwardOneStep(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStore(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	// open -> in_progress: OK (+1).
	if err := s.UpdateStatus(issue.ID, "in_progress"); err != nil {
		t.Errorf("open -> in_progress should succeed: %v", err)
	}
}

func TestFSMForwardSkip(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStore(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	// open -> delivered: ERROR (skips in_progress).
	if err := s.UpdateStatus(issue.ID, "delivered"); err == nil {
		t.Error("open -> delivered should fail (skips in_progress)")
	}
}

func TestFSMBackward(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStore(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	// Walk to delivered.
	_ = s.UpdateStatus(issue.ID, "in_progress")
	_ = s.UpdateStatus(issue.ID, "delivered")

	// delivered -> in_progress: OK (backward always allowed).
	if err := s.UpdateStatus(issue.ID, "in_progress"); err != nil {
		t.Errorf("delivered -> in_progress should succeed: %v", err)
	}
}

func TestFSMEscapeHatch(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStore(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	_ = s.UpdateStatus(issue.ID, "in_progress")
	_ = s.UpdateStatus(issue.ID, "delivered")

	// delivered -> rejected: OK (off-sequence entry).
	if err := s.UpdateStatus(issue.ID, "rejected"); err != nil {
		t.Errorf("delivered -> rejected should succeed: %v", err)
	}

	// rejected -> in_progress: OK (off-sequence exit).
	if err := s.UpdateStatus(issue.ID, "in_progress"); err != nil {
		t.Errorf("rejected -> in_progress should succeed: %v", err)
	}
}

func TestFSMBlockedExit(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStore(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	_ = s.UpdateStatus(issue.ID, "in_progress")

	// in_progress -> blocked: OK.
	if err := s.UpdateStatus(issue.ID, "blocked"); err != nil {
		t.Fatalf("in_progress -> blocked should succeed: %v", err)
	}

	// blocked -> in_progress: OK (allowed unblock target).
	if err := s.UpdateStatus(issue.ID, "in_progress"); err != nil {
		t.Errorf("blocked -> in_progress should succeed: %v", err)
	}

	// Block again.
	_ = s.UpdateStatus(issue.ID, "blocked")

	// blocked -> delivered: ERROR.
	if err := s.UpdateStatus(issue.ID, "delivered"); err == nil {
		t.Error("blocked -> delivered should fail")
	}

	// blocked -> accepted: ERROR.
	if err := s.UpdateStatus(issue.ID, "accepted"); err == nil {
		t.Error("blocked -> accepted should fail")
	}

	// blocked -> open: OK.
	if err := s.UpdateStatus(issue.ID, "open"); err != nil {
		t.Errorf("blocked -> open should succeed: %v", err)
	}
}

func TestFSMBlockedExitWithoutRules(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStoreNoExitRules(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	_ = s.UpdateStatus(issue.ID, "in_progress")
	_ = s.UpdateStatus(issue.ID, "blocked")

	// Without exit rules, blocked -> delivered is allowed (off-sequence, no restriction).
	if err := s.UpdateStatus(issue.ID, "delivered"); err != nil {
		t.Errorf("without exit rules, blocked -> delivered should succeed: %v", err)
	}
}

func TestFSMUndeferUsesConfiguredResumeStatus(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.SetConfigValue("status.custom", "delivered,accepted,rejected"); err != nil {
		t.Fatalf("set custom: %v", err)
	}
	if err := s.SetConfigValue("status.sequence", "open,in_progress,delivered,accepted,closed"); err != nil {
		t.Fatalf("set sequence: %v", err)
	}
	if err := s.SetConfigValue("status.exit_rules", "deferred:in_progress"); err != nil {
		t.Fatalf("set exit rules: %v", err)
	}
	if err := s.SetConfigValue("status.fsm", "true"); err != nil {
		t.Fatalf("enable fsm: %v", err)
	}

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	if err := s.UpdateStatus(issue.ID, "in_progress"); err != nil {
		t.Fatalf("move to in_progress: %v", err)
	}
	if err := s.DeferIssue(issue.ID, ""); err != nil {
		t.Fatalf("defer issue: %v", err)
	}
	if err := s.UnDeferIssue(issue.ID); err != nil {
		t.Fatalf("undefer issue: %v", err)
	}

	got, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("read issue: %v", err)
	}
	if got.Status != model.StatusInProgress {
		t.Fatalf("undefer should resume to in_progress, got %s", got.Status)
	}
}

func TestUpdateDescription_ReplacesDescriptionSectionOnly(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, err := s.CreateIssue("Test", "old description", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := s.AppendNotes(issue.ID, "keep these notes"); err != nil {
		t.Fatalf("AppendNotes: %v", err)
	}

	before, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("ReadIssue before update: %v", err)
	}
	beforeHash := before.ContentHash

	newDescription := "new description\nwith two lines"
	if err := s.UpdateDescription(issue.ID, newDescription); err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}

	after, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("ReadIssue after update: %v", err)
	}

	if !strings.Contains(after.Body, "## Description\nnew description\nwith two lines\n") {
		t.Fatalf("description section not updated correctly:\n%s", after.Body)
	}
	if !strings.Contains(after.Body, "## Acceptance Criteria") {
		t.Fatal("acceptance criteria section should be preserved")
	}
	if !strings.Contains(after.Body, "## Notes\nkeep these notes\n") {
		t.Fatalf("notes section should be preserved:\n%s", after.Body)
	}
	if after.ContentHash == beforeHash {
		t.Fatal("content hash should change when description changes")
	}
}

func TestFSMBlockedEntry(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStore(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	// open -> blocked: OK.
	if err := s.UpdateStatus(issue.ID, "blocked"); err != nil {
		t.Errorf("open -> blocked should succeed: %v", err)
	}

	_ = s.UpdateStatus(issue.ID, "in_progress")
	_ = s.UpdateStatus(issue.ID, "delivered")

	// delivered -> blocked: OK.
	if err := s.UpdateStatus(issue.ID, "blocked"); err != nil {
		t.Errorf("delivered -> blocked should succeed: %v", err)
	}
}

func TestFSMCloseEnforcement(t *testing.T) {
	dir := t.TempDir()
	s := setupFSMStore(t, dir)

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	_ = s.UpdateStatus(issue.ID, "in_progress")
	_ = s.UpdateStatus(issue.ID, "delivered")

	// CloseIssue from delivered: ERROR (must be at accepted first).
	if err := s.CloseIssue(issue.ID, "done"); err == nil {
		t.Error("close from delivered should fail (must go through accepted)")
	}

	// Walk to accepted.
	_ = s.UpdateStatus(issue.ID, "accepted")

	// CloseIssue from accepted: OK.
	if err := s.CloseIssue(issue.ID, "done"); err != nil {
		t.Errorf("close from accepted should succeed: %v", err)
	}
}

func TestFSMDisabled(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Set custom and sequence but NOT fsm.
	_ = s.SetConfigValue("status.custom", "delivered,accepted,rejected")
	_ = s.SetConfigValue("status.sequence", "open,in_progress,delivered,accepted,closed")

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")

	// Should allow skipping when FSM disabled.
	if err := s.UpdateStatus(issue.ID, "delivered"); err != nil {
		t.Errorf("with FSM disabled, open -> delivered should succeed: %v", err)
	}
}

func setupFSMStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.SetConfigValue("status.custom", "delivered,accepted,rejected"); err != nil {
		t.Fatalf("set custom: %v", err)
	}
	if err := s.SetConfigValue("status.sequence", "open,in_progress,delivered,accepted,closed"); err != nil {
		t.Fatalf("set sequence: %v", err)
	}
	if err := s.SetConfigValue("status.exit_rules", "blocked:open,in_progress,deferred;deferred:open,in_progress,deferred"); err != nil {
		t.Fatalf("set exit rules: %v", err)
	}
	if err := s.SetConfigValue("status.fsm", "true"); err != nil {
		t.Fatalf("enable fsm: %v", err)
	}
	return s
}

// setupFSMStoreNoExitRules creates an FSM store WITHOUT exit rules to test
// that the engine is generic and doesn't hardcode blocked/deferred behavior.
func setupFSMStoreNoExitRules(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.SetConfigValue("status.custom", "delivered,accepted,rejected"); err != nil {
		t.Fatalf("set custom: %v", err)
	}
	if err := s.SetConfigValue("status.sequence", "open,in_progress,delivered,accepted,closed"); err != nil {
		t.Fatalf("set sequence: %v", err)
	}
	if err := s.SetConfigValue("status.fsm", "true"); err != nil {
		t.Fatalf("enable fsm: %v", err)
	}
	return s
}

func TestAddRelated(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, err := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Add related link.
	if err := s.AddRelated(a.ID, b.ID); err != nil {
		t.Fatalf("AddRelated: %v", err)
	}

	// Verify bidirectional.
	aRead, _ := s.ReadIssue(a.ID)
	bRead, _ := s.ReadIssue(b.ID)

	if !contains(aRead.Related, b.ID) {
		t.Errorf("A.Related should contain B: %v", aRead.Related)
	}
	if !contains(bRead.Related, a.ID) {
		t.Errorf("B.Related should contain A: %v", bRead.Related)
	}

	// Verify wikilinks in body.
	if !strings.Contains(aRead.Body, "[["+b.ID+"]]") {
		t.Errorf("A body should contain wikilink to B:\n%s", aRead.Body)
	}
	if !strings.Contains(bRead.Body, "[["+a.ID+"]]") {
		t.Errorf("B body should contain wikilink to A:\n%s", bRead.Body)
	}

	// Idempotent: adding again should not duplicate.
	if err := s.AddRelated(a.ID, b.ID); err != nil {
		t.Fatalf("AddRelated idempotent: %v", err)
	}
	aRead2, _ := s.ReadIssue(a.ID)
	count := 0
	for _, r := range aRead2.Related {
		if r == b.ID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 related entry, got %d: %v", count, aRead2.Related)
	}

	// Self-reference should fail.
	if err := s.AddRelated(a.ID, a.ID); err == nil {
		t.Error("AddRelated to self should fail")
	}
}

func TestBuildLinksSectionFollows(t *testing.T) {
	tests := []struct {
		name  string
		issue *model.Issue
		want  []string
	}{
		{
			name:  "follows only",
			issue: &model.Issue{ID: "TST-0001", Follows: []string{"TST-pred"}},
			want:  []string{"- Follows: [[TST-pred]]"},
		},
		{
			name:  "led_to only",
			issue: &model.Issue{ID: "TST-0001", LedTo: []string{"TST-succ1", "TST-succ2"}},
			want:  []string{"- Led to: [[TST-succ1]], [[TST-succ2]]"},
		},
		{
			name: "both follows and led_to",
			issue: &model.Issue{
				ID:      "TST-0001",
				Follows: []string{"TST-pred"},
				LedTo:   []string{"TST-succ"},
			},
			want: []string{
				"- Follows: [[TST-pred]]",
				"- Led to: [[TST-succ]]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLinksSection(tt.issue)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
}

func TestNewIssueHasHistorySection(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, err := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(read.Body, "\n## History\n") {
		t.Errorf("new issue should have ## History section:\n%s", read.Body)
	}
}

func TestMarshalFrontmatterFollowsLedTo(t *testing.T) {
	issue := &model.Issue{
		ID:        "TST-0001",
		Title:     "Test",
		Status:    model.StatusOpen,
		Priority:  2,
		Type:      model.TypeTask,
		CreatedBy: "tester",
		Related:   []string{"TST-r1"},
		Follows:   []string{"TST-pred"},
		LedTo:     []string{"TST-succ"},
	}
	fm := marshalFrontmatter(issue)

	// Verify ordering: related before follows, follows before led_to.
	relIdx := strings.Index(fm, "related:")
	folIdx := strings.Index(fm, "follows:")
	ledIdx := strings.Index(fm, "led_to:")

	if relIdx < 0 || folIdx < 0 || ledIdx < 0 {
		t.Fatalf("missing fields in frontmatter:\n%s", fm)
	}
	if relIdx >= folIdx {
		t.Errorf("related (%d) should come before follows (%d)", relIdx, folIdx)
	}
	if folIdx >= ledIdx {
		t.Errorf("follows (%d) should come before led_to (%d)", folIdx, ledIdx)
	}
}

func TestAppendHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, err := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.AppendHistoryEntry(issue.ID, "test entry"); err != nil {
		t.Fatalf("AppendHistoryEntry: %v", err)
	}

	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(read.Body, "test entry") {
		t.Errorf("body should contain history entry:\n%s", read.Body)
	}
}

func TestAppendHistorySelfHealing(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, err := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Remove ## History section to simulate pre-existing issue.
	read, _ := s.ReadIssue(issue.ID)
	newBody := strings.Replace(read.Body, "\n## History\n\n", "", 1)
	if err := s.vault.Write(issue.ID, newBody, false); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify it's gone.
	read2, _ := s.ReadIssue(issue.ID)
	if strings.Contains(read2.Body, "\n## History\n") {
		t.Fatal("## History should be removed for this test")
	}

	// appendHistory should self-heal.
	if err := s.AppendHistoryEntry(issue.ID, "healed entry"); err != nil {
		t.Fatalf("AppendHistoryEntry: %v", err)
	}

	read3, _ := s.ReadIssue(issue.ID)
	if !strings.Contains(read3.Body, "\n## History\n") {
		t.Errorf("## History should be self-healed:\n%s", read3.Body)
	}
	if !strings.Contains(read3.Body, "healed entry") {
		t.Errorf("body should contain healed entry:\n%s", read3.Body)
	}
}

func TestAddFollowsBidirectional(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	if err := s.AddFollows(b.ID, a.ID); err != nil {
		t.Fatalf("AddFollows: %v", err)
	}

	bRead, _ := s.ReadIssue(b.ID)
	aRead, _ := s.ReadIssue(a.ID)

	if !contains(bRead.Follows, a.ID) {
		t.Errorf("B.Follows should contain A: %v", bRead.Follows)
	}
	if !contains(aRead.LedTo, b.ID) {
		t.Errorf("A.LedTo should contain B: %v", aRead.LedTo)
	}

	// Verify wikilinks in body.
	if !strings.Contains(bRead.Body, "Follows: [["+a.ID+"]]") {
		t.Errorf("B body should contain Follows wikilink:\n%s", bRead.Body)
	}
	if !strings.Contains(aRead.Body, "Led to: [["+b.ID+"]]") {
		t.Errorf("A body should contain Led to wikilink:\n%s", aRead.Body)
	}
}

func TestAddFollowsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	_ = s.AddFollows(b.ID, a.ID)
	_ = s.AddFollows(b.ID, a.ID) // second call

	bRead, _ := s.ReadIssue(b.ID)
	count := 0
	for _, f := range bRead.Follows {
		if f == a.ID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 follows entry, got %d: %v", count, bRead.Follows)
	}
}

func TestRemoveFollows(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	_ = s.AddFollows(b.ID, a.ID)
	if err := s.RemoveFollows(b.ID, a.ID); err != nil {
		t.Fatalf("RemoveFollows: %v", err)
	}

	bRead, _ := s.ReadIssue(b.ID)
	aRead, _ := s.ReadIssue(a.ID)

	if len(bRead.Follows) != 0 {
		t.Errorf("B.Follows should be empty: %v", bRead.Follows)
	}
	if len(aRead.LedTo) != 0 {
		t.Errorf("A.LedTo should be empty: %v", aRead.LedTo)
	}
}

func TestUpdateStatusAppendsHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	_ = s.UpdateStatus(issue.ID, model.StatusInProgress)

	read, _ := s.ReadIssue(issue.ID)
	if !strings.Contains(read.Body, "status: open -> in_progress") {
		t.Errorf("body should contain status transition history:\n%s", read.Body)
	}
}

func TestCloseAppendsHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	_ = s.CloseIssue(issue.ID, "done")

	read, _ := s.ReadIssue(issue.ID)
	if !strings.Contains(read.Body, "status: open -> closed") {
		t.Errorf("body should contain close history:\n%s", read.Body)
	}
}

func TestReopenAppendsHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, _ := s.CreateIssue("Test", "", "task", 2, "", nil, "")
	_ = s.CloseIssue(issue.ID, "")
	_ = s.ReopenIssue(issue.ID)

	read, _ := s.ReadIssue(issue.ID)
	if !strings.Contains(read.Body, "status: closed -> open (reopened)") {
		t.Errorf("body should contain reopen history:\n%s", read.Body)
	}
}

func TestAutoFollowsFromWasBlockedBy(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Design auth", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Implement auth", "", "task", 2, "", nil, "")

	// B blocked by A.
	_ = s.AddDependency(b.ID, a.ID)
	// Close A.
	_ = s.CloseIssue(a.ID, "done")
	// Remove dep (archives to was_blocked_by).
	_ = s.RemoveDependency(b.ID, a.ID)
	// Start B -- should auto-follow A.
	_ = s.UpdateStatus(b.ID, model.StatusInProgress)

	bRead, _ := s.ReadIssue(b.ID)
	if !contains(bRead.Follows, a.ID) {
		t.Errorf("B.Follows should contain A after auto-follow: %v", bRead.Follows)
	}

	aRead, _ := s.ReadIssue(a.ID)
	if !contains(aRead.LedTo, b.ID) {
		t.Errorf("A.LedTo should contain B after auto-follow: %v", aRead.LedTo)
	}
}

func TestAutoFollowsFromSibling(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	epic, _ := s.CreateIssue("Epic", "", "epic", 2, "", nil, "")
	a, _ := s.CreateIssue("Task A", "", "task", 2, "", nil, epic.ID)
	b, _ := s.CreateIssue("Task B", "", "task", 2, "", nil, epic.ID)

	// Close A.
	_ = s.CloseIssue(a.ID, "done")
	// Start B -- should auto-follow A (sibling fallback).
	_ = s.UpdateStatus(b.ID, model.StatusInProgress)

	bRead, _ := s.ReadIssue(b.ID)
	if !contains(bRead.Follows, a.ID) {
		t.Errorf("B.Follows should contain A (sibling auto-follow): %v", bRead.Follows)
	}
}

func TestAutoFollowsSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	epic, _ := s.CreateIssue("Epic", "", "epic", 2, "", nil, "")
	a, _ := s.CreateIssue("Task A", "", "task", 2, "", nil, epic.ID)
	b, _ := s.CreateIssue("Task B", "", "task", 2, "", nil, epic.ID)

	_ = s.CloseIssue(a.ID, "done")
	// Manually add follows first.
	_ = s.AddFollows(b.ID, a.ID)
	// Now start B -- auto-follow should skip A since already present.
	_ = s.UpdateStatus(b.ID, model.StatusInProgress)

	bRead, _ := s.ReadIssue(b.ID)
	count := 0
	for _, f := range bRead.Follows {
		if f == a.ID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 follows entry for A, got %d: %v", count, bRead.Follows)
	}
}

func TestDeleteIssueCleanupFollows(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	_ = s.AddFollows(b.ID, a.ID)

	// Delete B -- should clean up A.LedTo.
	_, err = s.DeleteIssue(b.ID, false)
	if err != nil {
		t.Fatalf("delete B: %v", err)
	}

	aRead, _ := s.ReadIssue(a.ID)
	if contains(aRead.LedTo, b.ID) {
		t.Errorf("A.LedTo should not contain deleted B: %v", aRead.LedTo)
	}
}

func TestDepAddAppendsHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	_ = s.AddDependency(b.ID, a.ID)

	bRead, _ := s.ReadIssue(b.ID)
	aRead, _ := s.ReadIssue(a.ID)

	if !strings.Contains(bRead.Body, "dep_added: blocked_by "+a.ID) {
		t.Errorf("B body should contain dep_added history:\n%s", bRead.Body)
	}
	if !strings.Contains(aRead.Body, "dep_added: blocks "+b.ID) {
		t.Errorf("A body should contain dep_added history:\n%s", aRead.Body)
	}
}

func TestAddDependencyIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	// First call -- should wire and record history.
	if err := s.AddDependency(b.ID, a.ID); err != nil {
		t.Fatalf("AddDependency first: %v", err)
	}

	// Second call -- should be a no-op.
	if err := s.AddDependency(b.ID, a.ID); err != nil {
		t.Fatalf("AddDependency second: %v", err)
	}

	// Verify only one blocked_by entry.
	bRead, _ := s.ReadIssue(b.ID)
	count := 0
	for _, bb := range bRead.BlockedBy {
		if bb == a.ID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 blocked_by entry, got %d: %v", count, bRead.BlockedBy)
	}

	// Verify only one dep_added history entry on B.
	depAddedCount := strings.Count(bRead.Body, "dep_added: blocked_by "+a.ID)
	if depAddedCount != 1 {
		t.Errorf("expected 1 dep_added history entry on B, got %d", depAddedCount)
	}

	// Verify only one dep_added history entry on A.
	aRead, _ := s.ReadIssue(a.ID)
	depAddedCountA := strings.Count(aRead.Body, "dep_added: blocks "+b.ID)
	if depAddedCountA != 1 {
		t.Errorf("expected 1 dep_added history entry on A, got %d", depAddedCountA)
	}
}

func TestSetParentIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	epic, _ := s.CreateIssue("Epic", "", "epic", 2, "", nil, "")
	child, _ := s.CreateIssue("Child", "", "task", 2, "", nil, "")

	// First call.
	if err := s.SetParent(child.ID, epic.ID); err != nil {
		t.Fatalf("SetParent first: %v", err)
	}
	read1, _ := s.ReadIssue(child.ID)
	updatedAt1 := read1.UpdatedAt

	// Second call -- should be a no-op (parent already set).
	if err := s.SetParent(child.ID, epic.ID); err != nil {
		t.Fatalf("SetParent second: %v", err)
	}
	read2, _ := s.ReadIssue(child.ID)

	// Parent should still be set.
	if read2.Parent != epic.ID {
		t.Errorf("parent = %q, want %q", read2.Parent, epic.ID)
	}

	// updated_at should NOT have changed (no-op).
	if read2.UpdatedAt != updatedAt1 {
		t.Errorf("updated_at changed on idempotent SetParent: %q -> %q", updatedAt1, read2.UpdatedAt)
	}
}

func TestAddFollowsIdempotentNoExtraLinks(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	// First call.
	_ = s.AddFollows(b.ID, a.ID)
	read1, _ := s.ReadIssue(b.ID)
	body1 := read1.Body

	// Second call -- should be a no-op.
	_ = s.AddFollows(b.ID, a.ID)
	read2, _ := s.ReadIssue(b.ID)

	// Body should not have changed (no extra UpdateLinksSection calls).
	if read2.Body != body1 {
		t.Errorf("body changed on idempotent AddFollows")
	}
}

func TestAddRelatedIdempotentNoExtraLinks(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	// First call.
	_ = s.AddRelated(a.ID, b.ID)
	readA1, _ := s.ReadIssue(a.ID)
	bodyA1 := readA1.Body

	// Second call -- should be a no-op.
	_ = s.AddRelated(a.ID, b.ID)
	readA2, _ := s.ReadIssue(a.ID)

	if readA2.Body != bodyA1 {
		t.Errorf("body changed on idempotent AddRelated")
	}
}

func TestCloseResolvesBlockers(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Blocker", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Dependent", "", "task", 2, "", nil, "")

	// B depends on A.
	if err := s.AddDependency(b.ID, a.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	// Close A.
	if err := s.CloseIssue(a.ID, "done"); err != nil {
		t.Fatalf("close A: %v", err)
	}

	// Cascade.
	unblocked, err := s.ResolveDependentsOf(a.ID)
	if err != nil {
		t.Fatalf("ResolveDependentsOf: %v", err)
	}
	if len(unblocked) != 1 || unblocked[0] != b.ID {
		t.Errorf("expected [%s], got %v", b.ID, unblocked)
	}

	// Verify B is unblocked.
	bRead, _ := s.ReadIssue(b.ID)
	if len(bRead.BlockedBy) != 0 {
		t.Errorf("B should have no blockers: %v", bRead.BlockedBy)
	}
	if !contains(bRead.WasBlockedBy, a.ID) {
		t.Errorf("B.WasBlockedBy should contain A: %v", bRead.WasBlockedBy)
	}

	// Verify A's blocks list is cleared.
	aRead, _ := s.ReadIssue(a.ID)
	if len(aRead.Blocks) != 0 {
		t.Errorf("A should have empty blocks: %v", aRead.Blocks)
	}

	// Verify history entries.
	if !strings.Contains(bRead.Body, "dep_removed: was_blocked_by "+a.ID) {
		t.Errorf("B body should contain dep_removed history:\n%s", bRead.Body)
	}
	if !strings.Contains(aRead.Body, "dep_removed: no_longer_blocks "+b.ID) {
		t.Errorf("A body should contain dep_removed history:\n%s", aRead.Body)
	}
}

func TestCloseResolvesMultipleBlockers(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Dependent", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Blocker B", "", "task", 2, "", nil, "")
	c, _ := s.CreateIssue("Blocker C", "", "task", 2, "", nil, "")

	// A depends on B and C.
	_ = s.AddDependency(a.ID, b.ID)
	_ = s.AddDependency(a.ID, c.ID)

	// Close B, cascade.
	_ = s.CloseIssue(b.ID, "done")
	unblocked, _ := s.ResolveDependentsOf(b.ID)
	if len(unblocked) != 1 || unblocked[0] != a.ID {
		t.Errorf("closing B should unblock A: got %v", unblocked)
	}

	// A should still be blocked by C.
	aRead, _ := s.ReadIssue(a.ID)
	if len(aRead.BlockedBy) != 1 || aRead.BlockedBy[0] != c.ID {
		t.Errorf("A should still be blocked by C: %v", aRead.BlockedBy)
	}

	// Close C, cascade.
	_ = s.CloseIssue(c.ID, "done")
	unblocked2, _ := s.ResolveDependentsOf(c.ID)
	if len(unblocked2) != 1 || unblocked2[0] != a.ID {
		t.Errorf("closing C should unblock A: got %v", unblocked2)
	}

	// A should be fully unblocked.
	aRead2, _ := s.ReadIssue(a.ID)
	if len(aRead2.BlockedBy) != 0 {
		t.Errorf("A should have no blockers: %v", aRead2.BlockedBy)
	}
	if !contains(aRead2.WasBlockedBy, b.ID) || !contains(aRead2.WasBlockedBy, c.ID) {
		t.Errorf("A.WasBlockedBy should contain B and C: %v", aRead2.WasBlockedBy)
	}
}

func TestCloseNoBlockersNoop(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Standalone", "", "task", 2, "", nil, "")
	_ = s.CloseIssue(a.ID, "done")

	unblocked, err := s.ResolveDependentsOf(a.ID)
	if err != nil {
		t.Fatalf("ResolveDependentsOf: %v", err)
	}
	if len(unblocked) != 0 {
		t.Errorf("expected no unblocked issues, got %v", unblocked)
	}
}

func TestDepRemoveAppendsHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	a, _ := s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	b, _ := s.CreateIssue("Issue B", "", "task", 2, "", nil, "")

	_ = s.AddDependency(b.ID, a.ID)
	_ = s.RemoveDependency(b.ID, a.ID)

	bRead, _ := s.ReadIssue(b.ID)
	aRead, _ := s.ReadIssue(a.ID)

	if !strings.Contains(bRead.Body, "dep_removed: was_blocked_by "+a.ID) {
		t.Errorf("B body should contain dep_removed history:\n%s", bRead.Body)
	}
	if !strings.Contains(aRead.Body, "dep_removed: no_longer_blocks "+b.ID) {
		t.Errorf("A body should contain dep_removed history:\n%s", aRead.Body)
	}
}
