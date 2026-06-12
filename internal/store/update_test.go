package store

import (
	"strings"
	"testing"
)

// authoredDescription mimics production story descriptions that embed their
// own copies of nd's canonical section headings. In live vaults these
// duplicates made vlt's heading-targeted Read/Patch abort with
// `heading "## Notes" is ambiguous`.
const authoredDescription = `Story context for the developer.

## Description
Authored nested description content.

## Acceptance Criteria
- [ ] authored criterion one
- [ ] authored criterion two

## Notes
Authored notes that belong to the story description.`

func TestAppendNotes_DuplicateAuthoredHeadings(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	issue, err := s.CreateIssue("Story with embedded headings", authoredDescription, "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	// This is the production failure: --append-notes aborted with
	// `heading "## Notes" is ambiguous: found 2 matches`.
	evidence := "delivery evidence: tests green"
	if err := s.AppendNotes(issue.ID, evidence); err != nil {
		t.Fatalf("AppendNotes with duplicate ## Notes heading: %v", err)
	}

	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("ReadIssue: %v", err)
	}

	canonicalNotes := strings.LastIndex(read.Body, "\n## Notes\n")
	if canonicalNotes < 0 {
		t.Fatalf("canonical ## Notes section missing:\n%s", read.Body)
	}
	evidenceIdx := strings.Index(read.Body, evidence)
	if evidenceIdx < 0 {
		t.Fatalf("appended notes content missing:\n%s", read.Body)
	}
	if evidenceIdx < canonicalNotes {
		t.Errorf("appended content landed in the authored ## Notes section, not the trailing canonical one:\n%s", read.Body)
	}
	historyIdx := strings.Index(read.Body, "\n## History\n")
	if historyIdx >= 0 && evidenceIdx > historyIdx {
		t.Errorf("appended content landed after ## History:\n%s", read.Body)
	}

	// Authored content must be untouched.
	if !strings.Contains(read.Body, "Authored notes that belong to the story description.") {
		t.Errorf("authored notes content lost:\n%s", read.Body)
	}

	// A second append must merge after the first inside the same section.
	if err := s.AppendNotes(issue.ID, "second entry"); err != nil {
		t.Fatalf("second AppendNotes: %v", err)
	}
	read2, _ := s.ReadIssue(issue.ID)
	if !strings.Contains(read2.Body, evidence+"\nsecond entry\n") {
		t.Errorf("second append should follow the first within the canonical section:\n%s", read2.Body)
	}
}

func TestAppendHistory_DuplicateAuthoredHeading(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	desc := "Context.\n\n## History\nAuthored historical background."
	issue, err := s.CreateIssue("Story with authored history", desc, "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := s.AppendHistoryEntry(issue.ID, "did the thing"); err != nil {
		t.Fatalf("AppendHistoryEntry with duplicate ## History heading: %v", err)
	}

	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("ReadIssue: %v", err)
	}

	canonicalHistory := strings.LastIndex(read.Body, "\n## History\n")
	entryIdx := strings.Index(read.Body, "did the thing")
	if entryIdx < 0 {
		t.Fatalf("history entry missing:\n%s", read.Body)
	}
	if entryIdx < canonicalHistory {
		t.Errorf("history entry landed in the authored ## History section:\n%s", read.Body)
	}
	if !strings.Contains(read.Body, "Authored historical background.") {
		t.Errorf("authored history content lost:\n%s", read.Body)
	}
}

func TestUpdateDescription_AuthoredDescriptionHeadingPreserved(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	desc := "intro line\n\n## Description\nAuthored nested description."
	issue, err := s.CreateIssue("Story with authored description", desc, "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := s.UpdateDescription(issue.ID, "replacement text"); err != nil {
		t.Fatalf("UpdateDescription with duplicate ## Description heading: %v", err)
	}

	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("ReadIssue: %v", err)
	}

	// Only the canonical first section's content is replaced, up to the
	// authored same-level heading.
	if !strings.Contains(read.Body, "## Description\nreplacement text\n") {
		t.Errorf("canonical description not replaced:\n%s", read.Body)
	}
	if strings.Contains(read.Body, "intro line") {
		t.Errorf("old canonical description content should be gone:\n%s", read.Body)
	}
	if !strings.Contains(read.Body, "Authored nested description.") {
		t.Errorf("authored duplicate section should be preserved:\n%s", read.Body)
	}
	if !strings.Contains(read.Body, "## Acceptance Criteria") {
		t.Errorf("structural sections should be preserved:\n%s", read.Body)
	}
}

func TestUpdateLinksSection_DuplicateAuthoredHeading(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	desc := "Context.\n\n## Links\n- authored link to [[SOMEWHERE]]"
	issue, err := s.CreateIssue("Story with authored links", desc, "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	parent, err := s.CreateIssue("Parent epic", "", "epic", 2, "", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}

	// SetParent triggers UpdateLinksSection, which previously aborted on a
	// duplicate ## Links heading the same way as AppendNotes.
	if err := s.SetParent(issue.ID, parent.ID); err != nil {
		t.Fatalf("SetParent with duplicate ## Links heading: %v", err)
	}

	read, err := s.ReadIssue(issue.ID)
	if err != nil {
		t.Fatalf("ReadIssue: %v", err)
	}

	canonicalLinks := strings.LastIndex(read.Body, "\n## Links\n")
	parentIdx := strings.Index(read.Body, "Parent: [["+parent.ID+"]]")
	if parentIdx < 0 {
		t.Fatalf("parent wikilink missing:\n%s", read.Body)
	}
	if parentIdx < canonicalLinks {
		t.Errorf("parent wikilink landed in the authored ## Links section:\n%s", read.Body)
	}
	if !strings.Contains(read.Body, "- authored link to [[SOMEWHERE]]") {
		t.Errorf("authored links content lost:\n%s", read.Body)
	}
}

func TestFindSectionBounds(t *testing.T) {
	body := "\n## Description\nauthored\n\n## Notes\nauthored notes\n\n## Notes\n\n\n## History\n"
	lines := strings.Split(body, "\n")

	// pickLast selects the trailing canonical section.
	headingIdx, contentEnd, found := findSectionBounds(lines, "## Notes", true)
	if !found {
		t.Fatal("last ## Notes not found")
	}
	if lines[headingIdx] != "## Notes" || headingIdx != 7 {
		t.Errorf("last ## Notes at line %d, want 7", headingIdx)
	}
	if lines[contentEnd] != "## History" {
		t.Errorf("section should end at ## History, got line %d (%q)", contentEnd, lines[contentEnd])
	}

	// pickLast=false selects the first occurrence.
	headingIdx, contentEnd, found = findSectionBounds(lines, "## Notes", false)
	if !found {
		t.Fatal("first ## Notes not found")
	}
	if headingIdx != 4 {
		t.Errorf("first ## Notes at line %d, want 4", headingIdx)
	}
	if lines[contentEnd] != "## Notes" {
		t.Errorf("first section should end at the duplicate ## Notes, got %q", lines[contentEnd])
	}

	// Level must match exactly: a ### sub-heading neither matches nor ends
	// a ## section.
	nested := strings.Split("## Notes\ncontent\n### Notes\nmore\n## History\n", "\n")
	headingIdx, contentEnd, found = findSectionBounds(nested, "## Notes", true)
	if !found || headingIdx != 0 {
		t.Fatalf("## Notes should match only the level-2 heading, got idx %d found %v", headingIdx, found)
	}
	if nested[contentEnd] != "## History" {
		t.Errorf("### sub-heading must not end the section, got %q", nested[contentEnd])
	}

	if _, _, found := findSectionBounds(lines, "## Missing", true); found {
		t.Error("missing heading should not be found")
	}
}
