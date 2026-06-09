package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/paivot-ai/nd/internal/enforce"
	"github.com/paivot-ai/nd/internal/store"
)

// Regression test: migrate must preserve the original updated_at from the
// JSONL. Every import pass (notes, sections, dependency wiring, epic
// promotion) touches updated_at, so restoration has to happen last.
func TestMigratePreservesUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ND_VAULT_DIR", dir)

	if _, err := store.Init(dir, "MIG", "tester"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	jsonl := filepath.Join(t.TempDir(), "beads.jsonl")
	lines := `{"id":"MIG-par1","title":"Epic","issue_type":"epic","priority":1,"description":"parent","created_at":"2025-01-02T03:04:05Z","updated_at":"2025-03-04T05:06:07Z"}
{"id":"MIG-chi1","title":"Child","issue_type":"task","priority":2,"description":"child","status":"closed","closed_at":"2025-02-03T04:05:06Z","created_at":"2025-01-02T03:04:05Z","updated_at":"2025-02-03T04:05:06Z","notes":"imported note","design":"imported design","dependencies":[{"issue_id":"MIG-chi1","depends_on_id":"MIG-par1","type":"parent-child"}]}
`
	if err := os.WriteFile(jsonl, []byte(lines), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	rootCmd.SetArgs([]string{"migrate", "--from-beads", jsonl})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	child, err := s.ReadIssue("MIG-chi1")
	if err != nil {
		t.Fatalf("read child: %v", err)
	}
	if got := child.UpdatedAt.UTC().Format(time.RFC3339); got != "2025-02-03T04:05:06Z" {
		t.Errorf("child updated_at = %s, want 2025-02-03T04:05:06Z", got)
	}
	if got := child.CreatedAt.UTC().Format(time.RFC3339); got != "2025-01-02T03:04:05Z" {
		t.Errorf("child created_at = %s, want 2025-01-02T03:04:05Z", got)
	}
	if got := child.ClosedAt; got != "2025-02-03T04:05:06Z" {
		t.Errorf("child closed_at = %s, want 2025-02-03T04:05:06Z", got)
	}
	if child.ContentHash != enforce.ComputeContentHash(child.Body) {
		t.Error("child content hash stale after migrate (notes/design patches must recompute it)")
	}

	parent, err := s.ReadIssue("MIG-par1")
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if got := parent.UpdatedAt.UTC().Format(time.RFC3339); got != "2025-03-04T05:06:07Z" {
		t.Errorf("parent updated_at = %s, want 2025-03-04T05:06:07Z (dependency wiring must not clobber it)", got)
	}
	if parent.ContentHash != enforce.ComputeContentHash(parent.Body) {
		t.Error("parent content hash stale after migrate")
	}
}
