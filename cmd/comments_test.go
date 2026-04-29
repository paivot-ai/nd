package cmd

import (
	"strings"
	"testing"

	"github.com/RamXX/nd/internal/store"
)

// TestCommentsList_FindsSectionAfterAdd is a regression test for the bug where
// comments list returned `heading "Comments" not found in <id>` even when
// comments add had successfully written the section. Root cause: vlt v0.8.x's
// findSection requires the markdown heading prefix ("## Comments"), but
// commentsListCmd was passing a bare "Comments" string. Fixed in
// cmd/comments.go.
func TestCommentsList_FindsSectionAfterAdd(t *testing.T) {
	dir := t.TempDir()

	if _, err := store.Init(dir, "TST", "tester"); err != nil {
		t.Fatalf("store.Init: %v", err)
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()

	// Create an issue.
	issue, err := s.CreateIssue("smoke", "body", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	// Append a comment using the same shape commentsAddCmd does.
	commentText := "first regression-test comment"
	comment := "\n### 2026-04-29T05:00:00Z tester\n" + commentText + "\n"
	if err := s.Vault().Append(issue.ID, comment, false); err != nil {
		t.Fatalf("Append comment: %v", err)
	}

	// Now read with the SAME heading argument commentsListCmd uses post-fix.
	content, err := s.Vault().Read(issue.ID, "## Comments")
	if err != nil {
		t.Fatalf("Read with '## Comments' must succeed after add, got: %v", err)
	}

	if !strings.Contains(content, commentText) {
		t.Errorf("returned section does not contain the appended comment\nwanted substring: %q\ngot:\n%s", commentText, content)
	}
	if !strings.Contains(content, "## Comments") {
		t.Errorf("returned section does not contain the heading line, got:\n%s", content)
	}
}

// TestCommentsList_BareHeadingFails documents WHY we keep the prefix. Drop
// this test when nd's vlt dependency is bumped past the version that adds
// level-insensitive heading lookup; until then, the bare-text variant must
// fail so the fix above is not silently regressed.
func TestCommentsList_BareHeadingFails(t *testing.T) {
	dir := t.TempDir()

	if _, err := store.Init(dir, "TST", "tester"); err != nil {
		t.Fatalf("store.Init: %v", err)
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()

	issue, err := s.CreateIssue("smoke", "body", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := s.Vault().Append(issue.ID, "\n### x\nhi\n", false); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if _, err := s.Vault().Read(issue.ID, "Comments"); err == nil {
		t.Fatal("bare 'Comments' lookup unexpectedly succeeded; vlt dep may have been bumped -- safe to drop this test and the prefix in commentsListCmd")
	}
}
