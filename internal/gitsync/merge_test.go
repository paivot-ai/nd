package gitsync

import (
	"strings"
	"testing"

	"github.com/paivot-ai/nd/internal/store"
)

func issueDoc(fm, body string) []byte {
	return []byte("---\n" + fm + "---\n" + body)
}

const baseFM = `id: T-a1b2
title: "Ship the feature"
status: open
priority: 2
type: feature
created_at: 2026-07-01T10:00:00Z
created_by: alice
updated_at: 2026-07-01T10:00:00Z
content_hash: "sha256:x"
`

const baseBody = `
## Description
Original description.

## Acceptance Criteria
- [ ] works

## Design

## Notes

## History
- 2026-07-01T10:00:00Z created

## Links

## Comments
`

func TestMergeListSetSemantics(t *testing.T) {
	base := []string{"A", "B", "C"}
	local := []string{"A", "C", "D"}       // removed B, added D
	remote := []string{"A", "B", "C", "E"} // added E

	got := mergeList(base, local, remote)
	want := []string{"A", "C", "D", "E"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("mergeList = %v, want %v", got, want)
	}
}

func TestMergeIssueOneSideScalarChanges(t *testing.T) {
	base := issueDoc(baseFM, baseBody)
	local := issueDoc(strings.Replace(baseFM,
		"status: open", "status: in_progress", 1), baseBody)
	local = []byte(strings.Replace(string(local),
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-02T10:00:00Z", 1))
	remote := issueDoc(strings.Replace(baseFM,
		"priority: 2", "priority: 0", 1), baseBody)
	remote = []byte(strings.Replace(string(remote),
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-03T10:00:00Z", 1))

	merged, notes := mergeIssueContent("issues/T-a1b2.md", base, local, remote)
	if len(notes) != 0 {
		t.Fatalf("unexpected notes: %v", notes)
	}
	issue, err := store.ParseIssueMarkdown(string(merged))
	if err != nil {
		t.Fatalf("parse merged: %v", err)
	}
	if string(issue.Status) != "in_progress" {
		t.Errorf("status = %s, want in_progress (local-only change survives)", issue.Status)
	}
	if int(issue.Priority) != 0 {
		t.Errorf("priority = %d, want 0 (remote-only change survives)", issue.Priority)
	}
	if issue.UpdatedAt.Format("2006-01-02") != "2026-07-03" {
		t.Errorf("updated_at = %s, want max of both sides", issue.UpdatedAt)
	}
}

func TestMergeIssueBothChangedScalarLWW(t *testing.T) {
	base := issueDoc(baseFM, baseBody)
	local := issueDoc(strings.NewReplacer(
		"status: open", "status: blocked",
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-02T10:00:00Z",
	).Replace(baseFM), baseBody)
	remote := issueDoc(strings.NewReplacer(
		"status: open", "status: in_progress",
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-05T10:00:00Z",
	).Replace(baseFM), baseBody)

	merged, _ := mergeIssueContent("issues/T-a1b2.md", base, local, remote)
	issue, err := store.ParseIssueMarkdown(string(merged))
	if err != nil {
		t.Fatalf("parse merged: %v", err)
	}
	if string(issue.Status) != "in_progress" {
		t.Errorf("status = %s, want in_progress (remote is newer)", issue.Status)
	}
}

func TestMergeIssueHistoryAndCommentsUnion(t *testing.T) {
	base := issueDoc(baseFM, baseBody)

	localBody := strings.Replace(baseBody,
		"- 2026-07-01T10:00:00Z created",
		"- 2026-07-01T10:00:00Z created\n- 2026-07-02T10:00:00Z status: open -> in_progress", 1)
	localBody += "\n### 2026-07-02T11:00:00Z alice\nStarted work.\n"
	local := issueDoc(strings.Replace(baseFM,
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-02T11:00:00Z", 1), localBody)

	remoteBody := strings.Replace(baseBody,
		"- 2026-07-01T10:00:00Z created",
		"- 2026-07-01T10:00:00Z created\n- 2026-07-03T09:00:00Z dep_added: blocked_by T-zzz", 1)
	remoteBody += "\n### 2026-07-03T09:30:00Z bob\nAdded a blocker.\n"
	remote := issueDoc(strings.Replace(baseFM,
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-03T09:30:00Z", 1), remoteBody)

	merged, notes := mergeIssueContent("issues/T-a1b2.md", base, local, remote)
	if len(notes) != 0 {
		t.Fatalf("unexpected notes: %v", notes)
	}
	out := string(merged)

	for _, want := range []string{
		"- 2026-07-01T10:00:00Z created",
		"- 2026-07-02T10:00:00Z status: open -> in_progress",
		"- 2026-07-03T09:00:00Z dep_added: blocked_by T-zzz",
		"### 2026-07-02T11:00:00Z alice",
		"### 2026-07-03T09:30:00Z bob",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("merged issue missing %q", want)
		}
	}

	// History must be timestamp-ordered.
	hIdx1 := strings.Index(out, "2026-07-02T10:00:00Z status")
	hIdx2 := strings.Index(out, "2026-07-03T09:00:00Z dep_added")
	if hIdx1 > hIdx2 {
		t.Error("history entries not in timestamp order")
	}
}

func TestMergeIssueSectionConflictLWW(t *testing.T) {
	base := issueDoc(baseFM, baseBody)
	local := issueDoc(strings.Replace(baseFM,
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-02T10:00:00Z", 1),
		strings.Replace(baseBody, "Original description.", "Local rewrite.", 1))
	remote := issueDoc(strings.Replace(baseFM,
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-04T10:00:00Z", 1),
		strings.Replace(baseBody, "Original description.", "Remote rewrite.", 1))

	merged, notes := mergeIssueContent("issues/T-a1b2.md", base, local, remote)
	out := string(merged)
	if !strings.Contains(out, "Remote rewrite.") {
		t.Error("expected remote (newer) description to win")
	}
	if strings.Contains(out, "Local rewrite.") {
		t.Error("losing description should not survive")
	}
	if len(notes) == 0 {
		t.Error("expected a conflict note")
	}
	if !strings.Contains(out, "sync-merge:") {
		t.Error("expected conflict recorded in History")
	}
}

func TestMergeIssueDependencyListsUnion(t *testing.T) {
	base := issueDoc(baseFM, baseBody)
	local := issueDoc(strings.Replace(baseFM,
		"type: feature", "type: feature\nblocked_by: [T-l1]", 1), baseBody)
	remote := issueDoc(strings.Replace(baseFM,
		"type: feature", "type: feature\nblocked_by: [T-r1]\nlabels: [urgent]", 1), baseBody)

	merged, _ := mergeIssueContent("issues/T-a1b2.md", base, local, remote)
	issue, err := store.ParseIssueMarkdown(string(merged))
	if err != nil {
		t.Fatalf("parse merged: %v", err)
	}
	if len(issue.BlockedBy) != 2 {
		t.Errorf("blocked_by = %v, want union of both sides", issue.BlockedBy)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "urgent" {
		t.Errorf("labels = %v, want [urgent]", issue.Labels)
	}
}

func TestMergeIssueContentHashRecomputed(t *testing.T) {
	base := issueDoc(baseFM, baseBody)
	local := issueDoc(baseFM, strings.Replace(baseBody, "Original description.", "Local rewrite.", 1))
	remote := issueDoc(strings.Replace(baseFM,
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-04T10:00:00Z", 1), baseBody)

	merged, _ := mergeIssueContent("issues/T-a1b2.md", base, local, remote)
	issue, err := store.ParseIssueMarkdown(string(merged))
	if err != nil {
		t.Fatalf("parse merged: %v", err)
	}
	if issue.ContentHash == "sha256:x" || issue.ContentHash == "" {
		t.Errorf("content_hash not recomputed: %q", issue.ContentHash)
	}
}

func TestMergeNotesLineUnion(t *testing.T) {
	got := mergeLines3(
		"keep\ndrop-local\ndrop-remote",
		"keep\ndrop-remote\nadded-local",
		"keep\ndrop-local\nadded-remote",
	)
	want := "keep\nadded-local\nadded-remote"
	if got != want {
		t.Fatalf("mergeLines3 = %q, want %q", got, want)
	}
}

func TestHasDuplicateHeadingsFallback(t *testing.T) {
	dupBody := baseBody + "\n## Notes\nauthored duplicate\n"
	base := issueDoc(baseFM, baseBody)
	local := issueDoc(strings.Replace(baseFM,
		"updated_at: 2026-07-01T10:00:00Z", "updated_at: 2026-07-05T10:00:00Z", 1), dupBody)
	remote := issueDoc(baseFM, strings.Replace(baseBody, "Original description.", "Remote edit.", 1))

	merged, notes := mergeIssueContent("issues/T-a1b2.md", base, local, remote)
	if len(notes) == 0 || !strings.Contains(notes[0], "duplicated section headings") {
		t.Fatalf("expected wholesale fallback note, got %v", notes)
	}
	if !strings.Contains(string(merged), "authored duplicate") {
		t.Error("expected newer (local) side kept wholesale")
	}
}
