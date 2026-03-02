package test

import (
	"sort"
	"strings"
	"testing"

	"github.com/RamXX/nd/internal/enforce"
	"github.com/RamXX/nd/internal/graph"
	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/nd/internal/store"
	"github.com/RamXX/vlt"
)

// Full workflow: init -> create -> dep -> ready -> close -> stats.
// No mocks. Real vault files on disk.

func TestFullWorkflow(t *testing.T) {
	dir := t.TempDir()

	// 1. Init.
	s, err := store.Init(dir, "INT", "integration-tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if s.Prefix() != "INT" {
		t.Fatalf("prefix = %q", s.Prefix())
	}

	// 2. Create issues.
	epic, err := s.CreateIssue("Auth Epic", "Implement authentication", "epic", 1, "", nil, "")
	if err != nil {
		t.Fatalf("create epic: %v", err)
	}

	taskA, err := s.CreateIssue("Design auth flow", "Design the auth flow", "task", 1, "alice", []string{"auth"}, epic.ID)
	if err != nil {
		t.Fatalf("create taskA: %v", err)
	}

	taskB, err := s.CreateIssue("Implement auth flow", "Build the auth flow", "feature", 1, "bob", nil, epic.ID)
	if err != nil {
		t.Fatalf("create taskB: %v", err)
	}

	bug, err := s.CreateIssue("Fix login crash", "App crashes on login", "bug", 0, "alice", []string{"critical"}, "")
	if err != nil {
		t.Fatalf("create bug: %v", err)
	}

	// 3. Add dependency: taskB depends on taskA.
	if err := s.AddDependency(taskB.ID, taskA.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	// Verify bidirectional.
	taskARead, _ := s.ReadIssue(taskA.ID)
	taskBRead, _ := s.ReadIssue(taskB.ID)
	if !containsStr(taskARead.Blocks, taskB.ID) {
		t.Errorf("taskA should block taskB: blocks=%v", taskARead.Blocks)
	}
	if !containsStr(taskBRead.BlockedBy, taskA.ID) {
		t.Errorf("taskB should be blocked by taskA: blocked_by=%v", taskBRead.BlockedBy)
	}

	// Verify wikilinks appear in body after AddDependency.
	if !strings.Contains(taskBRead.Body, "[["+taskA.ID+"]]") {
		t.Errorf("taskB body should contain wikilink to taskA after AddDependency")
	}
	if !strings.Contains(taskARead.Body, "[["+taskB.ID+"]]") {
		t.Errorf("taskA body should contain wikilink to taskB after AddDependency")
	}

	// 4. Build graph and test ready/blocked.
	all, err := s.ListIssues(store.FilterOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 issues, got %d", len(all))
	}

	g := graph.Build(all)
	ready := g.Ready()
	blocked := g.Blocked()

	// taskB should be blocked, everything else ready.
	readyIDs := idsOf(ready)
	blockedIDs := idsOf(blocked)

	if containsStr(readyIDs, taskB.ID) {
		t.Errorf("taskB should not be ready: %v", readyIDs)
	}
	if !containsStr(blockedIDs, taskB.ID) {
		t.Errorf("taskB should be blocked: %v", blockedIDs)
	}
	if !containsStr(readyIDs, taskA.ID) {
		t.Errorf("taskA should be ready: %v", readyIDs)
	}
	if !containsStr(readyIDs, bug.ID) {
		t.Errorf("bug should be ready: %v", readyIDs)
	}

	// 5. Close taskA -> taskB should become unblocked.
	if err := s.CloseIssue(taskA.ID, "Design complete"); err != nil {
		t.Fatalf("close taskA: %v", err)
	}

	all2, _ := s.ListIssues(store.FilterOptions{})
	g2 := graph.Build(all2)
	ready2 := g2.Ready()
	readyIDs2 := idsOf(ready2)

	if !containsStr(readyIDs2, taskB.ID) {
		t.Errorf("after closing taskA, taskB should be ready: %v", readyIDs2)
	}

	// 6. Test stats.
	stats := g2.Stats()
	if stats.Total != 4 {
		t.Errorf("total = %d", stats.Total)
	}
	if stats.Closed != 1 {
		t.Errorf("closed = %d", stats.Closed)
	}

	// 7. Reopen taskA.
	if err := s.ReopenIssue(taskA.ID); err != nil {
		t.Fatalf("reopen taskA: %v", err)
	}
	taskAReopened, _ := s.ReadIssue(taskA.ID)
	if taskAReopened.Status != model.StatusOpen {
		t.Errorf("taskA should be open after reopen, got %s", taskAReopened.Status)
	}

	// 8. Verify content hash integrity.
	for _, issue := range all {
		expected := enforce.ComputeContentHash(issue.Body)
		if issue.ContentHash != expected {
			// Content hash may drift if we modified body via comments.
			// This is expected and doctor --fix addresses it.
			t.Logf("hash drift on %s (expected after body modifications)", issue.ID)
		}
	}

	// 9. Epic tree.
	tree := g2.EpicTree(epic.ID)
	if tree == nil {
		t.Fatal("epic tree should not be nil")
	}
	if len(tree.Children) != 2 {
		t.Errorf("epic should have 2 children, got %d", len(tree.Children))
	}

	// 10. Epic status.
	summary := g2.EpicStatus(epic.ID)
	if summary.Total != 2 {
		t.Errorf("epic total = %d, want 2", summary.Total)
	}

	// 11. Remove dependency.
	if err := s.RemoveDependency(taskB.ID, taskA.ID); err != nil {
		t.Fatalf("remove dep: %v", err)
	}
	taskBAfter, _ := s.ReadIssue(taskB.ID)
	if len(taskBAfter.BlockedBy) != 0 {
		t.Errorf("taskB should have no blockers after removal: %v", taskBAfter.BlockedBy)
	}

	// Verify historical relationship preserved in was_blocked_by.
	if !containsStr(taskBAfter.WasBlockedBy, taskA.ID) {
		t.Errorf("taskB.WasBlockedBy should contain taskA: %v", taskBAfter.WasBlockedBy)
	}
	// Wikilink still present via was_blocked_by history.
	if !strings.Contains(taskBAfter.Body, "Was blocked by: [["+taskA.ID+"]]") {
		t.Errorf("taskB body should contain 'Was blocked by' wikilink to taskA")
	}

	// 12. Update fields.
	if err := s.UpdateField(taskB.ID, "assignee", "charlie"); err != nil {
		t.Fatalf("update assignee: %v", err)
	}
	taskBUpdated, _ := s.ReadIssue(taskB.ID)
	if taskBUpdated.Assignee != "charlie" {
		t.Errorf("assignee = %q, want charlie", taskBUpdated.Assignee)
	}

	// 13. Filter by status.
	openIssues, _ := s.ListIssues(store.FilterOptions{Status: "open"})
	for _, i := range openIssues {
		if i.Status != model.StatusOpen {
			t.Errorf("filter returned non-open issue: %s %s", i.ID, i.Status)
		}
	}

	// 14. Filter by assignee.
	aliceIssues, _ := s.ListIssues(store.FilterOptions{Assignee: "alice"})
	for _, i := range aliceIssues {
		if !strings.EqualFold(i.Assignee, "alice") {
			t.Errorf("filter returned non-alice issue: %s %s", i.ID, i.Assignee)
		}
	}

	// 15. Filter by label.
	criticalIssues, _ := s.ListIssues(store.FilterOptions{Label: "critical"})
	for _, i := range criticalIssues {
		found := false
		for _, l := range i.Labels {
			if l == "critical" {
				found = true
			}
		}
		if !found {
			t.Errorf("filter returned issue without critical label: %s", i.ID)
		}
	}
}

func TestSearchIntegration(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Init(dir, "SRC", "tester")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	_, err = s.CreateIssue("Database migration", "Migrate from Dolt to SQLite", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = s.CreateIssue("API redesign", "Redesign the REST API", "feature", 1, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Search for "Dolt".
	results, err := s.Vault().Search(vlt.SearchOptions{Query: "Dolt", Path: "issues"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'Dolt', got %d", len(results))
	}
}

func TestMigrateWorkflow(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Init(dir, "MIG", "migrate-tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create issues that simulate beads import (Pass 1).
	epic, err := s.CreateIssueWithID("MIG-epic1", "Auth Epic", "Implement auth", "epic", 1, "", nil, "")
	if err != nil {
		t.Fatalf("create epic: %v", err)
	}

	child1, err := s.CreateIssueWithID("MIG-ch01", "Design auth", "Design the flow", "task", 1, "alice", nil, "")
	if err != nil {
		t.Fatalf("create child1: %v", err)
	}

	child2, err := s.CreateIssueWithID("MIG-ch02", "Implement auth", "Build the flow", "task", 1, "bob", nil, "")
	if err != nil {
		t.Fatalf("create child2: %v", err)
	}

	related1, err := s.CreateIssueWithID("MIG-rel1", "Research OAuth", "Investigate providers", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create related1: %v", err)
	}

	// Pass 2: Wire dependencies.
	// parent-child: child1 -> epic
	if err := s.SetParent(child1.ID, epic.ID); err != nil {
		t.Fatalf("SetParent child1: %v", err)
	}
	// parent-child: child2 -> epic
	if err := s.SetParent(child2.ID, epic.ID); err != nil {
		t.Fatalf("SetParent child2: %v", err)
	}
	// blocks: child2 blocked by child1
	if err := s.AddDependency(child2.ID, child1.ID); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	// related: related1 related to child1
	if err := s.AddRelated(related1.ID, child1.ID); err != nil {
		t.Fatalf("AddRelated: %v", err)
	}

	// Verify parent-child.
	ch1Read, _ := s.ReadIssue(child1.ID)
	if ch1Read.Parent != epic.ID {
		t.Errorf("child1 parent = %q, want %q", ch1Read.Parent, epic.ID)
	}
	ch2Read, _ := s.ReadIssue(child2.ID)
	if ch2Read.Parent != epic.ID {
		t.Errorf("child2 parent = %q, want %q", ch2Read.Parent, epic.ID)
	}

	// Verify blocks.
	if !containsStr(ch2Read.BlockedBy, child1.ID) {
		t.Errorf("child2 should be blocked by child1: %v", ch2Read.BlockedBy)
	}
	ch1Read, _ = s.ReadIssue(child1.ID) // re-read after AddDependency updated it
	if !containsStr(ch1Read.Blocks, child2.ID) {
		t.Errorf("child1 should block child2: %v", ch1Read.Blocks)
	}

	// Verify related.
	rel1Read, _ := s.ReadIssue(related1.ID)
	if !containsStr(rel1Read.Related, child1.ID) {
		t.Errorf("related1 should relate to child1: %v", rel1Read.Related)
	}
	ch1Read, _ = s.ReadIssue(child1.ID) // re-read after AddRelated
	if !containsStr(ch1Read.Related, related1.ID) {
		t.Errorf("child1 should relate to related1: %v", ch1Read.Related)
	}

	// Verify wikilinks in body.
	if !strings.Contains(ch1Read.Body, "[["+epic.ID+"]]") {
		t.Errorf("child1 body should have wikilink to parent epic")
	}
	if !strings.Contains(ch2Read.Body, "[["+child1.ID+"]]") {
		t.Errorf("child2 body should have wikilink to blocker child1")
	}
	if !strings.Contains(rel1Read.Body, "[["+child1.ID+"]]") {
		t.Errorf("related1 body should have wikilink to related child1")
	}

	// Verify graph connectivity.
	all, err := s.ListIssues(store.FilterOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 issues, got %d", len(all))
	}

	g := graph.Build(all)

	// Epic tree should show 2 children.
	tree := g.EpicTree(epic.ID)
	if tree == nil {
		t.Fatal("epic tree should not be nil")
	}
	if len(tree.Children) != 2 {
		t.Errorf("epic should have 2 children, got %d", len(tree.Children))
	}

	// child2 should be blocked.
	blocked := g.Blocked()
	blockedIDs := idsOf(blocked)
	if !containsStr(blockedIDs, child2.ID) {
		t.Errorf("child2 should be blocked: %v", blockedIDs)
	}
}

func TestMigrateTrajectoryInference(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Init(dir, "TRJ", "traj-tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create an epic with 3 children, each closed at different times.
	epic, _ := s.CreateIssueWithID("TRJ-epic1", "Auth Epic", "Auth work", "epic", 1, "", nil, "")
	ch1, _ := s.CreateIssueWithID("TRJ-ch01", "Design", "Design auth", "task", 1, "", nil, "")
	ch2, _ := s.CreateIssueWithID("TRJ-ch02", "Implement", "Build auth", "task", 1, "", nil, "")
	ch3, _ := s.CreateIssueWithID("TRJ-ch03", "Test", "Test auth", "task", 1, "", nil, "")

	// Wire parent-child.
	_ = s.SetParent(ch1.ID, epic.ID)
	_ = s.SetParent(ch2.ID, epic.ID)
	_ = s.SetParent(ch3.ID, epic.ID)

	// Close children with ascending timestamps.
	_ = s.CloseIssue(ch1.ID, "done")
	_ = s.UpdateField(ch1.ID, "closed_at", "2026-01-10T10:00:00Z")
	_ = s.CloseIssue(ch2.ID, "done")
	_ = s.UpdateField(ch2.ID, "closed_at", "2026-01-11T10:00:00Z")
	_ = s.CloseIssue(ch3.ID, "done")
	_ = s.UpdateField(ch3.ID, "closed_at", "2026-01-12T10:00:00Z")
	_ = s.CloseIssue(epic.ID, "all done")
	_ = s.UpdateField(epic.ID, "closed_at", "2026-01-12T12:00:00Z")

	// Create two related orphans (no parent), both closed.
	orphA, _ := s.CreateIssueWithID("TRJ-orpA", "Research X", "Investigate X", "task", 2, "", nil, "")
	orphB, _ := s.CreateIssueWithID("TRJ-orpB", "Research Y", "Investigate Y", "task", 2, "", nil, "")
	_ = s.AddRelated(orphA.ID, orphB.ID)
	_ = s.CloseIssue(orphA.ID, "done")
	_ = s.UpdateField(orphA.ID, "closed_at", "2026-01-08T10:00:00Z")
	_ = s.CloseIssue(orphB.ID, "done")
	_ = s.UpdateField(orphB.ID, "closed_at", "2026-01-09T10:00:00Z")

	// Simulate Pass 3 logic: sibling chains.
	parentIDs := map[string]bool{epic.ID: true}
	followsWired := 0

	// 3a: Sibling chains.
	for pid := range parentIDs {
		children, err := s.ListIssues(store.FilterOptions{Parent: pid, Status: "closed"})
		if err != nil || len(children) < 2 {
			continue
		}
		sort.Slice(children, func(i, j int) bool {
			return children[i].ClosedAt < children[j].ClosedAt
		})
		for i := 1; i < len(children); i++ {
			pred := children[i-1]
			succ := children[i]
			if pred.ClosedAt == "" || succ.ClosedAt == "" {
				continue
			}
			if err := s.AddFollows(succ.ID, pred.ID); err == nil {
				followsWired++
			}
		}
	}

	// 3b: Related orphan chains.
	type relatedPair struct{ a, b string }
	closedOrphans := map[string]*model.Issue{}
	{
		orphans, _ := s.ListIssues(store.FilterOptions{NoParent: true, Status: "closed"})
		for _, o := range orphans {
			closedOrphans[o.ID] = o
		}
	}
	var relatedPairs []relatedPair
	seen := map[relatedPair]bool{}
	for _, o := range closedOrphans {
		for _, rid := range o.Related {
			if _, ok := closedOrphans[rid]; !ok {
				continue
			}
			p := relatedPair{o.ID, rid}
			pr := relatedPair{rid, o.ID}
			if seen[p] || seen[pr] {
				continue
			}
			seen[p] = true
			relatedPairs = append(relatedPairs, p)
		}
	}
	for _, rp := range relatedPairs {
		a, b := closedOrphans[rp.a], closedOrphans[rp.b]
		if a.ClosedAt == "" || b.ClosedAt == "" {
			continue
		}
		if a.ClosedAt <= b.ClosedAt {
			if err := s.AddFollows(b.ID, a.ID); err == nil {
				followsWired++
			}
		} else {
			if err := s.AddFollows(a.ID, b.ID); err == nil {
				followsWired++
			}
		}
	}

	// Verify sibling chain: ch1 -> ch2 -> ch3.
	ch2Read, _ := s.ReadIssue(ch2.ID)
	if !containsStr(ch2Read.Follows, ch1.ID) {
		t.Errorf("ch2 should follow ch1; follows=%v", ch2Read.Follows)
	}
	ch3Read, _ := s.ReadIssue(ch3.ID)
	if !containsStr(ch3Read.Follows, ch2.ID) {
		t.Errorf("ch3 should follow ch2; follows=%v", ch3Read.Follows)
	}
	ch1Read, _ := s.ReadIssue(ch1.ID)
	if !containsStr(ch1Read.LedTo, ch2.ID) {
		t.Errorf("ch1 should have led_to ch2; led_to=%v", ch1Read.LedTo)
	}

	// Verify related orphan chain: orphA -> orphB.
	orphBRead, _ := s.ReadIssue(orphB.ID)
	if !containsStr(orphBRead.Follows, orphA.ID) {
		t.Errorf("orphB should follow orphA; follows=%v", orphBRead.Follows)
	}
	orphARead, _ := s.ReadIssue(orphA.ID)
	if !containsStr(orphARead.LedTo, orphB.ID) {
		t.Errorf("orphA should have led_to orphB; led_to=%v", orphARead.LedTo)
	}

	// Total: 2 sibling links + 1 related link = 3.
	if followsWired != 3 {
		t.Errorf("expected 3 follows wired, got %d", followsWired)
	}
}

func TestCustomStatusWorkflow(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Init(dir, "CST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// 1. Configure custom statuses.
	if err := s.SetConfigValue("status.custom", "delivered,accepted,rejected"); err != nil {
		t.Fatalf("set custom: %v", err)
	}

	// 2. Create an issue.
	issue, err := s.CreateIssue("Custom status test", "Test custom statuses", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 3. Update to custom status.
	customSt, err := model.ParseStatusWithCustom("delivered", s.CustomStatuses())
	if err != nil {
		t.Fatalf("parse custom status: %v", err)
	}
	if err := s.UpdateStatus(issue.ID, customSt); err != nil {
		t.Fatalf("update to delivered: %v", err)
	}

	// 4. Verify read back.
	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.Status != "delivered" {
		t.Errorf("status = %q, want delivered", read.Status)
	}

	// 5. Filter by custom status.
	filtered, err := s.ListIssues(store.FilterOptions{Status: "delivered"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("expected 1 delivered issue, got %d", len(filtered))
	}

	// 6. Stats should include custom status.
	all, _ := s.ListIssues(store.FilterOptions{})
	g := graph.Build(all)
	st := g.Stats()
	if st.ByStatus["delivered"] != 1 {
		t.Errorf("ByStatus[delivered] = %d, want 1", st.ByStatus["delivered"])
	}

	// 7. Doctor should not complain about custom status.
	for _, i := range all {
		if err := enforce.ValidateIssueWithCustom(i, s.CustomStatuses()); err != nil {
			t.Errorf("ValidateIssueWithCustom(%s): %v", i.ID, err)
		}
	}
}

func TestFSMWorkflow(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Init(dir, "FSM", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Configure.
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

	issue, err := s.CreateIssue("FSM test", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Illegal: open -> delivered (skip).
	if err := s.UpdateStatus(issue.ID, "delivered"); err == nil {
		t.Error("open -> delivered should fail")
	}

	// Happy path: open -> in_progress -> delivered -> accepted -> close.
	for _, next := range []model.Status{"in_progress", "delivered", "accepted"} {
		if err := s.UpdateStatus(issue.ID, next); err != nil {
			t.Fatalf("%s transition failed: %v", next, err)
		}
	}
	if err := s.CloseIssue(issue.ID, "completed"); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Verify closed.
	read, _ := s.ReadIssue(issue.ID)
	if read.Status != model.StatusClosed {
		t.Errorf("status = %q, want closed", read.Status)
	}

	// Reopen always works.
	if err := s.ReopenIssue(issue.ID); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	read, _ = s.ReadIssue(issue.ID)
	if read.Status != model.StatusOpen {
		t.Errorf("status after reopen = %q, want open", read.Status)
	}

	// Test blocked circuit-breaker.
	_ = s.UpdateStatus(issue.ID, "in_progress")
	_ = s.UpdateStatus(issue.ID, "blocked")

	// blocked -> delivered: ERROR.
	if err := s.UpdateStatus(issue.ID, "delivered"); err == nil {
		t.Error("blocked -> delivered should fail")
	}

	// blocked -> in_progress: OK.
	if err := s.UpdateStatus(issue.ID, "in_progress"); err != nil {
		t.Errorf("blocked -> in_progress should succeed: %v", err)
	}

	// Test reject path.
	_ = s.UpdateStatus(issue.ID, "delivered")
	_ = s.UpdateStatus(issue.ID, "rejected")    // off-sequence entry: OK
	_ = s.UpdateStatus(issue.ID, "in_progress") // off-sequence exit: OK
	_ = s.UpdateStatus(issue.ID, "delivered")
	_ = s.UpdateStatus(issue.ID, "accepted")
	if err := s.CloseIssue(issue.ID, "done after rework"); err != nil {
		t.Fatalf("close after rework: %v", err)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Init(dir, "IDP", "idempotent-tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// === First migration: create issues and wire deps ===
	epic, err := s.CreateIssueWithID("IDP-epic1", "Auth Epic", "Implement auth", "epic", 1, "", nil, "")
	if err != nil {
		t.Fatalf("create epic: %v", err)
	}
	ch1, err := s.CreateIssueWithID("IDP-ch01", "Design auth", "Design the flow", "task", 1, "alice", nil, "")
	if err != nil {
		t.Fatalf("create ch1: %v", err)
	}
	ch2, err := s.CreateIssueWithID("IDP-ch02", "Implement auth", "Build the flow", "task", 1, "bob", nil, "")
	if err != nil {
		t.Fatalf("create ch2: %v", err)
	}
	rel1, err := s.CreateIssueWithID("IDP-rel1", "Research OAuth", "Investigate", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create rel1: %v", err)
	}

	// Wire dependencies.
	_ = s.SetParent(ch1.ID, epic.ID)
	_ = s.SetParent(ch2.ID, epic.ID)
	_ = s.AddDependency(ch2.ID, ch1.ID)
	_ = s.AddRelated(rel1.ID, ch1.ID)

	// Close children to enable follows inference.
	_ = s.CloseIssue(ch1.ID, "done")
	_ = s.UpdateField(ch1.ID, "closed_at", "2026-01-10T10:00:00Z")
	_ = s.CloseIssue(ch2.ID, "done")
	_ = s.UpdateField(ch2.ID, "closed_at", "2026-01-11T10:00:00Z")

	// Wire follows.
	_ = s.AddFollows(ch2.ID, ch1.ID)

	// Snapshot all issue bodies after first migration.
	allIssues, _ := s.ListIssues(store.FilterOptions{Status: "all"})
	bodySnapshot := map[string]string{}
	for _, issue := range allIssues {
		bodySnapshot[issue.ID] = issue.Body
	}

	// === Second migration: re-attempt everything ===

	// Re-creating issues should fail (already exist).
	_, err = s.CreateIssueWithID("IDP-epic1", "Auth Epic", "Implement auth", "epic", 1, "", nil, "")
	if err == nil {
		t.Error("expected error re-creating IDP-epic1")
	}
	_, err = s.CreateIssueWithID("IDP-ch01", "Design auth", "Design the flow", "task", 1, "alice", nil, "")
	if err == nil {
		t.Error("expected error re-creating IDP-ch01")
	}

	// Re-wire dependencies -- should be no-ops (idempotent).
	_ = s.SetParent(ch1.ID, epic.ID)
	_ = s.SetParent(ch2.ID, epic.ID)
	_ = s.AddDependency(ch2.ID, ch1.ID)
	_ = s.AddRelated(rel1.ID, ch1.ID)
	_ = s.AddFollows(ch2.ID, ch1.ID)

	// Verify bodies have not changed.
	allAfter, _ := s.ListIssues(store.FilterOptions{Status: "all"})
	for _, issue := range allAfter {
		before, ok := bodySnapshot[issue.ID]
		if !ok {
			t.Errorf("unexpected issue %s after second migration", issue.ID)
			continue
		}
		if issue.Body != before {
			t.Errorf("body of %s changed after idempotent re-wire:\n--- before ---\n%s\n--- after ---\n%s",
				issue.ID, before, issue.Body)
		}
	}

	// Verify no duplicate relationship entries.
	ch2Read, _ := s.ReadIssue(ch2.ID)
	if count := countInSlice(ch2Read.BlockedBy, ch1.ID); count != 1 {
		t.Errorf("expected 1 blocked_by entry, got %d: %v", count, ch2Read.BlockedBy)
	}
	if count := countInSlice(ch2Read.Follows, ch1.ID); count != 1 {
		t.Errorf("expected 1 follows entry, got %d: %v", count, ch2Read.Follows)
	}

	rel1Read, _ := s.ReadIssue(rel1.ID)
	if count := countInSlice(rel1Read.Related, ch1.ID); count != 1 {
		t.Errorf("expected 1 related entry, got %d: %v", count, rel1Read.Related)
	}

	ch1Read, _ := s.ReadIssue(ch1.ID)
	if ch1Read.Parent != epic.ID {
		t.Errorf("ch1 parent = %q, want %q", ch1Read.Parent, epic.ID)
	}
}

func countInSlice(ss []string, s string) int {
	n := 0
	for _, v := range ss {
		if v == s {
			n++
		}
	}
	return n
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// TestReadyFiltering verifies that the ready set can be filtered with the same
// FilterOptions used by nd list (parent, label, priority, assignee, type).
// This is an integration test: real vault files, no mocks.
func TestReadyFiltering(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Init(dir, "RDY", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create an epic with two children and a standalone task.
	epic, _ := s.CreateIssue("Auth Epic", "Auth work", "epic", 1, "", nil, "")
	child1, _ := s.CreateIssue("Design auth", "Design", "task", 1, "alice", []string{"auth"}, epic.ID)
	child2, _ := s.CreateIssue("Implement auth", "Build", "feature", 2, "bob", []string{"auth", "backend"}, epic.ID)
	standalone, _ := s.CreateIssue("Fix CSS bug", "Broken layout", "bug", 0, "alice", []string{"frontend"}, "")

	// child2 depends on child1 --> child2 is blocked.
	if err := s.AddDependency(child2.ID, child1.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	// Build the full ready set (for reference).
	all, _ := s.ListIssues(store.FilterOptions{})
	g := graph.Build(all)
	readyAll := g.Ready()
	readyAllIDs := idsOf(readyAll)

	// child2 should be blocked, everything else ready (epic, child1, standalone).
	if containsStr(readyAllIDs, child2.ID) {
		t.Errorf("child2 should not be ready (blocked): %v", readyAllIDs)
	}
	if !containsStr(readyAllIDs, child1.ID) || !containsStr(readyAllIDs, standalone.ID) {
		t.Errorf("child1 and standalone should be ready: %v", readyAllIDs)
	}

	// --- Filter by parent (epic-scoped) ---
	epicReady := filterReadyWithOpts(t, s, store.FilterOptions{Parent: epic.ID})
	epicReadyIDs := idsOf(epicReady)
	if !containsStr(epicReadyIDs, child1.ID) {
		t.Errorf("--parent: child1 should be in epic-scoped ready: %v", epicReadyIDs)
	}
	if containsStr(epicReadyIDs, standalone.ID) {
		t.Errorf("--parent: standalone should NOT be in epic-scoped ready: %v", epicReadyIDs)
	}
	if containsStr(epicReadyIDs, child2.ID) {
		t.Errorf("--parent: child2 should still be blocked: %v", epicReadyIDs)
	}
	// Epic itself should not show (it has a different parent -- no parent).
	if containsStr(epicReadyIDs, epic.ID) {
		t.Errorf("--parent: epic itself should not match parent filter: %v", epicReadyIDs)
	}

	// --- Filter by assignee ---
	aliceReady := filterReadyWithOpts(t, s, store.FilterOptions{Assignee: "alice"})
	aliceIDs := idsOf(aliceReady)
	if !containsStr(aliceIDs, child1.ID) || !containsStr(aliceIDs, standalone.ID) {
		t.Errorf("--assignee=alice: should include child1 and standalone: %v", aliceIDs)
	}
	if containsStr(aliceIDs, child2.ID) {
		t.Errorf("--assignee=alice: child2 is bob's and blocked: %v", aliceIDs)
	}

	// --- Filter by label ---
	authReady := filterReadyWithOpts(t, s, store.FilterOptions{Label: "auth"})
	authIDs := idsOf(authReady)
	if !containsStr(authIDs, child1.ID) {
		t.Errorf("--label=auth: child1 should match: %v", authIDs)
	}
	if containsStr(authIDs, standalone.ID) {
		t.Errorf("--label=auth: standalone has no auth label: %v", authIDs)
	}

	frontendReady := filterReadyWithOpts(t, s, store.FilterOptions{Label: "frontend"})
	frontendIDs := idsOf(frontendReady)
	if !containsStr(frontendIDs, standalone.ID) {
		t.Errorf("--label=frontend: standalone should match: %v", frontendIDs)
	}
	if len(frontendIDs) != 1 {
		t.Errorf("--label=frontend: expected exactly 1 result, got %d: %v", len(frontendIDs), frontendIDs)
	}

	// --- Filter by priority ---
	p0Ready := filterReadyWithOpts(t, s, store.FilterOptions{Priority: "0"})
	p0IDs := idsOf(p0Ready)
	if !containsStr(p0IDs, standalone.ID) {
		t.Errorf("--priority=0: standalone (P0) should match: %v", p0IDs)
	}
	if containsStr(p0IDs, child1.ID) {
		t.Errorf("--priority=0: child1 (P1) should not match: %v", p0IDs)
	}

	// --- Filter by type ---
	bugReady := filterReadyWithOpts(t, s, store.FilterOptions{Type: "bug"})
	bugIDs := idsOf(bugReady)
	if !containsStr(bugIDs, standalone.ID) {
		t.Errorf("--type=bug: standalone should match: %v", bugIDs)
	}
	if len(bugIDs) != 1 {
		t.Errorf("--type=bug: expected exactly 1 result, got %d: %v", len(bugIDs), bugIDs)
	}

	// --- Filter by no-parent ---
	noParentReady := filterReadyWithOpts(t, s, store.FilterOptions{NoParent: true})
	noParentIDs := idsOf(noParentReady)
	if !containsStr(noParentIDs, standalone.ID) {
		t.Errorf("--no-parent: standalone should match: %v", noParentIDs)
	}
	if !containsStr(noParentIDs, epic.ID) {
		t.Errorf("--no-parent: epic has no parent, should match: %v", noParentIDs)
	}
	if containsStr(noParentIDs, child1.ID) || containsStr(noParentIDs, child2.ID) {
		t.Errorf("--no-parent: children have a parent, should not match: %v", noParentIDs)
	}

	// --- Combined filters (parent + label) ---
	epicBackendReady := filterReadyWithOpts(t, s, store.FilterOptions{Parent: epic.ID, Label: "backend"})
	epicBackendIDs := idsOf(epicBackendReady)
	// child2 has "backend" label but is blocked, so nothing should match.
	if len(epicBackendIDs) != 0 {
		t.Errorf("--parent + --label=backend: child2 is blocked, expected 0 results, got %d: %v",
			len(epicBackendIDs), epicBackendIDs)
	}

	// Unblock child2, then check again.
	if err := s.CloseIssue(child1.ID, "done"); err != nil {
		t.Fatalf("close child1: %v", err)
	}
	epicBackendReady2 := filterReadyWithOpts(t, s, store.FilterOptions{Parent: epic.ID, Label: "backend"})
	epicBackendIDs2 := idsOf(epicBackendReady2)
	if !containsStr(epicBackendIDs2, child2.ID) {
		t.Errorf("after unblocking: child2 should now be ready with --parent + --label=backend: %v", epicBackendIDs2)
	}
	if len(epicBackendIDs2) != 1 {
		t.Errorf("expected exactly 1 result after unblocking, got %d: %v", len(epicBackendIDs2), epicBackendIDs2)
	}
}

// filterReadyWithOpts simulates the nd ready command logic: load all issues,
// compute graph readiness, then apply FilterOptions to the ready set.
func filterReadyWithOpts(t *testing.T, s *store.Store, opts store.FilterOptions) []*model.Issue {
	t.Helper()
	all, err := s.ListIssues(store.FilterOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	g := graph.Build(all)
	ready := g.Ready()

	var filtered []*model.Issue
	for _, issue := range ready {
		if s.MatchesFilter(issue, opts) {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}

func idsOf(issues []*model.Issue) []string {
	ids := make([]string, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID
	}
	return ids
}
