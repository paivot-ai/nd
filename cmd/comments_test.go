package cmd

import (
	"strings"
	"testing"

	"github.com/paivot-ai/nd/internal/store"
)

// TestCommentsList_FindsSectionAfterAdd is a regression test for the bug where
// comments list returned `heading "Comments" not found in <id>` even when
// comments add had successfully written the section. vlt's findSection now
// supports level-insensitive lookup, so a bare "Comments" heading resolves the
// "## Comments" section written by comments add.
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

	// Read with the same bare heading argument commentsListCmd uses.
	res, err := s.Vault().Read(issue.ID, "Comments")
	if err != nil {
		t.Fatalf("Read 'Comments' must succeed after add, got: %v", err)
	}
	content := res.Content

	if !strings.Contains(content, commentText) {
		t.Errorf("returned section does not contain the appended comment\nwanted substring: %q\ngot:\n%s", commentText, content)
	}
	if !strings.Contains(content, "## Comments") {
		t.Errorf("returned section does not contain the heading line, got:\n%s", content)
	}
}
