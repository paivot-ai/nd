package store

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/paivot-ai/nd/internal/enforce"
	"github.com/paivot-ai/nd/internal/model"
)

// UpdateField updates a single frontmatter field on an issue.
func (s *Store) UpdateField(id, field, value string) error {
	if err := s.vault.PropertySet(id, field, value); err != nil {
		return fmt.Errorf("set %s on %s: %w", field, id, err)
	}
	return s.touchUpdatedAt(id)
}

// UpdateStatus changes the status of an issue with validation.
func (s *Store) UpdateStatus(id string, newStatus model.Status) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}

	oldStatus := issue.Status

	// Validate transition.
	if issue.Status == model.StatusClosed && newStatus != model.StatusOpen {
		return fmt.Errorf("closed issues can only be reopened (set to open)")
	}

	if s.config.StatusFSM {
		if err := s.validateFSMTransition(issue.Status, newStatus); err != nil {
			return err
		}
	}

	if err := s.vault.PropertySet(id, "status", string(newStatus)); err != nil {
		return err
	}
	if err := s.touchUpdatedAt(id); err != nil {
		return err
	}

	_ = s.appendHistory(id, fmt.Sprintf("status: %s -> %s", oldStatus, newStatus))

	if newStatus == model.StatusInProgress {
		preds := s.detectPredecessors(issue)
		for _, predID := range preds {
			if err := s.AddFollows(id, predID); err == nil {
				_ = s.appendHistory(id, fmt.Sprintf("auto-follows: linked to predecessor %s", predID))
			}
		}
	}

	return nil
}

// CloseIssue closes an issue with an optional reason.
func (s *Store) CloseIssue(id, reason string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}
	if issue.Status == model.StatusClosed {
		return fmt.Errorf("issue %s is already closed", id)
	}

	if s.config.StatusFSM {
		if err := s.validateFSMTransition(issue.Status, model.StatusClosed); err != nil {
			return err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.vault.PropertySet(id, "status", "closed"); err != nil {
		return err
	}
	if err := s.vault.PropertySet(id, "closed_at", now); err != nil {
		return err
	}
	if reason != "" {
		if err := s.vault.PropertySet(id, "close_reason", fmt.Sprintf("%q", reason)); err != nil {
			return err
		}
	}
	if err := s.touchUpdatedAt(id); err != nil {
		return err
	}
	_ = s.appendHistory(id, fmt.Sprintf("status: %s -> closed", issue.Status))
	return nil
}

// ReopenIssue changes a closed issue back to open.
func (s *Store) ReopenIssue(id string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}
	if issue.Status != model.StatusClosed {
		return fmt.Errorf("issue %s is not closed (status: %s)", id, issue.Status)
	}

	if err := s.vault.PropertySet(id, "status", "open"); err != nil {
		return err
	}
	// Clear closed_at and close_reason.
	_ = s.vault.PropertyRemove(id, "closed_at")
	_ = s.vault.PropertyRemove(id, "close_reason")
	if err := s.touchUpdatedAt(id); err != nil {
		return err
	}
	_ = s.appendHistory(id, "status: closed -> open (reopened)")
	return nil
}

// AppendNotes appends text to the Notes section. Self-heals issues that lack
// the ## Notes section (e.g. imported from other trackers).
func (s *Store) AppendNotes(id, content string) error {
	if err := s.ensureSection(id, "## Notes", "\n## History\n"); err != nil {
		return err
	}
	if err := s.appendToSection(id, "## Notes", content); err != nil {
		return err
	}
	return s.touchUpdatedAt(id)
}

// appendToSection appends content to the end of a section and recomputes the
// content hash. It operates on the raw issue body and targets the LAST
// occurrence of the heading: authored story descriptions frequently embed
// their own "## Notes"/"## History" headings, and nd's canonical structural
// sections always trail them. Delegating to vlt's heading-targeted Read/Patch
// would abort on such duplicates ("heading is ambiguous").
func (s *Store) appendToSection(id, heading, content string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}

	existing, found := sectionContent(issue.Body, heading, true)
	if !found {
		return fmt.Errorf("append to %s: heading %q not found", id, heading)
	}

	merged := content
	if strings.TrimSpace(existing) != "" {
		merged = existing + "\n" + content
	}

	newBody, err := replaceSectionContent(issue.Body, heading, merged+"\n", true)
	if err != nil {
		return fmt.Errorf("append to %s: %w", id, err)
	}
	if err := s.vault.Write(id, newBody, false); err != nil {
		return err
	}
	return s.RecomputeContentHash(id)
}

// headingLevel returns the ATX heading level of a line (the number of leading
// '#' characters followed by a space or end of line), or 0 when the line is
// not a heading.
func headingLevel(line string) int {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 {
		return 0
	}
	if level >= len(trimmed) || trimmed[level] == ' ' {
		return level
	}
	return 0
}

// findSectionBounds locates a markdown section by exact heading-line match.
// heading must include its "#" prefix (e.g. "## Notes") so the level is
// explicit. The section spans from the heading line to the line before the
// next heading of equal or higher level (or EOF). When the heading appears
// more than once, pickLast selects the trailing occurrence -- correct for
// nd's canonical structural sections (## Notes, ## History, ## Links), which
// always trail authored duplicates embedded in the description -- while
// pickLast=false selects the first occurrence (canonical for ## Description).
func findSectionBounds(lines []string, heading string, pickLast bool) (headingIdx, contentEnd int, found bool) {
	level := headingLevel(heading)
	if level == 0 {
		return 0, 0, false
	}

	headingIdx = -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			headingIdx = i
			if !pickLast {
				break
			}
		}
	}
	if headingIdx < 0 {
		return 0, 0, false
	}

	contentEnd = len(lines)
	for i := headingIdx + 1; i < len(lines); i++ {
		if lvl := headingLevel(lines[i]); lvl > 0 && lvl <= level {
			contentEnd = i
			break
		}
	}
	return headingIdx, contentEnd, true
}

// sectionContent returns the content of the selected section without its
// heading line, trimmed of trailing newlines.
func sectionContent(body, heading string, pickLast bool) (string, bool) {
	lines := strings.Split(body, "\n")
	headingIdx, contentEnd, found := findSectionBounds(lines, heading, pickLast)
	if !found {
		return "", false
	}
	return strings.TrimRight(strings.Join(lines[headingIdx+1:contentEnd], "\n"), "\n"), true
}

// replaceSectionContent replaces the body of the section selected by heading
// and pickLast with content, keeping the heading line. content is split on
// newlines and spliced in verbatim; empty content removes the section body.
// This mirrors vlt.Patch's heading-targeted splice, but resolves duplicate
// headings deterministically instead of erroring.
func replaceSectionContent(body, heading, content string, pickLast bool) (string, error) {
	lines := strings.Split(body, "\n")
	headingIdx, contentEnd, found := findSectionBounds(lines, heading, pickLast)
	if !found {
		return "", fmt.Errorf("heading %q not found", heading)
	}

	result := make([]string, 0, len(lines))
	result = append(result, lines[:headingIdx+1]...)
	if content != "" {
		result = append(result, strings.Split(content, "\n")...)
	}
	result = append(result, lines[contentEnd:]...)
	return strings.Join(result, "\n"), nil
}

// ensureSection inserts an empty section into the body when missing: before
// anchor when the anchor exists, otherwise at the end of the body.
func (s *Store) ensureSection(id, heading, anchor string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}
	if strings.Contains(issue.Body, "\n"+heading+"\n") || strings.HasPrefix(issue.Body, heading+"\n") {
		return nil
	}

	var newBody string
	if idx := strings.Index(issue.Body, anchor); idx >= 0 {
		newBody = issue.Body[:idx] + "\n" + heading + "\n\n" + issue.Body[idx:]
	} else {
		newBody = strings.TrimRight(issue.Body, "\n") + "\n\n" + heading + "\n"
	}
	return s.vault.Write(id, newBody, false)
}

// RecomputeContentHash re-reads the body and stores its hash. Must be called
// after any mutation of the issue body that bypasses UpdateDescription,
// UpdateBody, or UpdateLinksSection, otherwise nd doctor reports drift.
func (s *Store) RecomputeContentHash(id string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}
	hash := enforce.ComputeContentHash(issue.Body)
	return s.vault.PropertySet(id, "content_hash", fmt.Sprintf("%q", hash))
}

// AddComment appends a timestamped, attributed comment to the issue body and
// keeps content hash and updated_at in sync. The ## Comments section is the
// last section of the body, so appending to the file lands inside it.
func (s *Store) AddComment(id, text string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	comment := fmt.Sprintf("\n### %s %s\n%s\n", now, s.config.CreatedBy, text)
	if err := s.vault.Append(id, comment, false); err != nil {
		return err
	}
	if err := s.RecomputeContentHash(id); err != nil {
		return err
	}
	return s.touchUpdatedAt(id)
}

// UpdateDescription replaces the content of the Description section while
// preserving the rest of the issue body. The canonical ## Description is the
// FIRST occurrence: authored duplicates live inside the description content,
// after it. Replacement extends only to the next heading of equal or higher
// level.
func (s *Store) UpdateDescription(id, description string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}

	newBody, err := replaceSectionContent(issue.Body, "## Description", description+"\n", false)
	if err != nil {
		return fmt.Errorf("update description on %s: %w", id, err)
	}
	if err := s.vault.Write(id, newBody, false); err != nil {
		return err
	}

	if err := s.RecomputeContentHash(id); err != nil {
		return err
	}
	return s.touchUpdatedAt(id)
}

// UpdateBody replaces the body and recalculates the content hash.
// The body is normalized to end with a newline before both the write and the
// hash: vlt's Write guarantees a trailing newline on disk, so hashing the
// un-normalized input would make nd doctor report drift on the next read.
func (s *Store) UpdateBody(id, body string) error {
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := s.vault.Write(id, body, false); err != nil {
		return err
	}
	hash := enforce.ComputeContentHash(body)
	if err := s.vault.PropertySet(id, "content_hash", fmt.Sprintf("%q", hash)); err != nil {
		return err
	}
	return s.touchUpdatedAt(id)
}

// UpdateLinksSection rebuilds the ## Links section from frontmatter relationships.
// The canonical ## Links is the LAST occurrence: authored duplicates live
// inside the description, before it. The section content is replaced
// wholesale since it is derived entirely from frontmatter.
func (s *Store) UpdateLinksSection(id string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}

	content := buildLinksSection(issue)
	body := issue.Body

	// Self-heal: insert ## Links before ## Comments when missing.
	if !strings.Contains(body, "\n## Links\n") {
		if idx := strings.Index(body, "\n## Comments\n"); idx >= 0 {
			body = body[:idx] + "\n## Links\n\n" + body[idx:]
		}
	}

	newBody, err := replaceSectionContent(body, "## Links", content, true)
	if err != nil {
		return fmt.Errorf("update links on %s: %w", id, err)
	}
	if err := s.vault.Write(id, newBody, false); err != nil {
		return err
	}

	// Recompute content hash after Links section update.
	return s.RecomputeContentHash(id)
}

// SetParent sets the parent of an issue and updates the Links section.
func (s *Store) SetParent(id, parentID string) error {
	// Early return if parent is already set to the requested value.
	issue, err := s.ReadIssue(id)
	if err == nil && issue.Parent == parentID {
		return nil
	}

	if parentID == "" {
		if err := s.vault.PropertyRemove(id, "parent"); err != nil {
			return err
		}
	} else {
		if err := s.vault.PropertySet(id, "parent", parentID); err != nil {
			return err
		}
	}
	if err := s.touchUpdatedAt(id); err != nil {
		return err
	}
	return s.UpdateLinksSection(id)
}

// RefreshAfterEdit recomputes the content hash and updates the Links section
// after a manual edit. Call this after an external editor modifies the file.
func (s *Store) RefreshAfterEdit(id string) error {
	if err := s.UpdateLinksSection(id); err != nil {
		return err
	}
	return s.touchUpdatedAt(id)
}

// DeferIssue sets the issue status to deferred with an optional until date.
func (s *Store) DeferIssue(id, until string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}
	if issue.Status == model.StatusClosed {
		return fmt.Errorf("cannot defer closed issue %s", id)
	}
	if s.config.StatusFSM {
		if err := s.validateFSMTransition(issue.Status, model.StatusDeferred); err != nil {
			return err
		}
	}

	if err := s.vault.PropertySet(id, "status", "deferred"); err != nil {
		return err
	}
	if until != "" {
		if err := s.vault.PropertySet(id, "defer_until", until); err != nil {
			return err
		}
	}
	if err := s.touchUpdatedAt(id); err != nil {
		return err
	}
	_ = s.appendHistory(id, fmt.Sprintf("status: %s -> deferred", issue.Status))
	return nil
}

// UnDeferIssue restores a deferred issue to open.
func (s *Store) UnDeferIssue(id string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return err
	}
	if issue.Status != model.StatusDeferred {
		return fmt.Errorf("issue %s is not deferred (status: %s)", id, issue.Status)
	}
	targetStatus := s.resumeStatusFromDeferred()
	if s.config.StatusFSM {
		if err := s.validateFSMTransition(issue.Status, targetStatus); err != nil {
			return err
		}
	}

	if err := s.vault.PropertySet(id, "status", string(targetStatus)); err != nil {
		return err
	}
	_ = s.vault.PropertyRemove(id, "defer_until")
	if err := s.touchUpdatedAt(id); err != nil {
		return err
	}
	_ = s.appendHistory(id, fmt.Sprintf("status: deferred -> %s", targetStatus))
	return nil
}

func (s *Store) touchUpdatedAt(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return s.vault.PropertySet(id, "updated_at", now)
}

// validateFSMTransition enforces the FSM transition rules.
// The engine is generic -- all behavior is driven by configuration:
//   - status.sequence: forward +1 only, backward any
//   - status.exit_rules: restrict exits from specific statuses to listed targets
//   - Off-sequence statuses are unrestricted (escape hatch)
func (s *Store) validateFSMTransition(from, to model.Status) error {
	// Check exit rules first -- these override sequence logic.
	exitRules := s.ExitRules()
	if allowed, ok := exitRules[from]; ok {
		for _, a := range allowed {
			if a == to {
				return nil
			}
		}
		targets := make([]string, len(allowed))
		for i, a := range allowed {
			targets[i] = string(a)
		}
		return fmt.Errorf("FSM: cannot transition from %s to %s; allowed targets: %s",
			from, to, strings.Join(targets, ", "))
	}

	seq := s.StatusSequence()
	if len(seq) == 0 {
		return nil
	}

	fromIdx := indexInSequence(seq, from)
	toIdx := indexInSequence(seq, to)

	// Both in sequence: forward must be +1, backward is always allowed.
	if fromIdx >= 0 && toIdx >= 0 {
		if toIdx > fromIdx {
			if toIdx != fromIdx+1 {
				return fmt.Errorf("FSM: cannot skip from %s to %s; next step is %s", from, to, seq[fromIdx+1])
			}
		}
		return nil
	}

	// One or both off-sequence: allow (escape hatch for custom statuses like rejected).
	return nil
}

func indexInSequence(seq []model.Status, st model.Status) int {
	for i, s := range seq {
		if s == st {
			return i
		}
	}
	return -1
}

func (s *Store) resumeStatusFromDeferred() model.Status {
	if targets, ok := s.ExitRules()[model.StatusDeferred]; ok && len(targets) > 0 {
		for _, target := range targets {
			if target == model.StatusOpen {
				return model.StatusOpen
			}
		}
		return targets[0]
	}
	return model.StatusOpen
}

// appendHistory appends a timestamped entry to the ## History section of an issue.
// Self-heals pre-existing issues that lack the ## History section.
func (s *Store) appendHistory(id, entry string) error {
	line := fmt.Sprintf("- %s %s", time.Now().UTC().Format(time.RFC3339), entry)

	if err := s.ensureSection(id, "## History", "\n## Links\n"); err != nil {
		return err
	}

	return s.appendToSection(id, "## History", line)
}

// AppendHistoryEntry appends a timestamped entry to the ## History section (public API).
func (s *Store) AppendHistoryEntry(id, entry string) error {
	return s.appendHistory(id, entry)
}

// AddFollows creates a bidirectional follows/led_to link between two issues.
// id follows predecessorID (predecessorID led to id).
func (s *Store) AddFollows(id, predecessorID string) error {
	if id == predecessorID {
		return fmt.Errorf("an issue cannot follow itself")
	}

	issue, err := s.ReadIssue(id)
	if err != nil {
		return fmt.Errorf("issue %s: %w", id, err)
	}
	pred, err := s.ReadIssue(predecessorID)
	if err != nil {
		return fmt.Errorf("predecessor %s: %w", predecessorID, err)
	}

	changed := false
	if !contains(issue.Follows, predecessorID) {
		newList := append(issue.Follows, predecessorID)
		if err := s.setListProperty(id, "follows", newList); err != nil {
			return err
		}
		changed = true
	}

	if !contains(pred.LedTo, id) {
		newList := append(pred.LedTo, id)
		if err := s.setListProperty(predecessorID, "led_to", newList); err != nil {
			return err
		}
		changed = true
	}

	if changed {
		_ = s.UpdateLinksSection(id)
		_ = s.UpdateLinksSection(predecessorID)
	}
	return nil
}

// RemoveFollows removes a bidirectional follows/led_to link between two issues.
func (s *Store) RemoveFollows(id, predecessorID string) error {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return fmt.Errorf("issue %s: %w", id, err)
	}
	pred, err := s.ReadIssue(predecessorID)
	if err != nil {
		return fmt.Errorf("predecessor %s: %w", predecessorID, err)
	}

	newFollows := remove(issue.Follows, predecessorID)
	if err := s.setListProperty(id, "follows", newFollows); err != nil {
		return err
	}

	newLedTo := remove(pred.LedTo, id)
	if err := s.setListProperty(predecessorID, "led_to", newLedTo); err != nil {
		return err
	}

	_ = s.UpdateLinksSection(id)
	_ = s.UpdateLinksSection(predecessorID)
	return nil
}

// detectPredecessors finds likely predecessor issues for auto-follows.
// Strategy 1: Closed issues from was_blocked_by not already in Follows.
// Strategy 2: Most recently closed sibling under same parent.
func (s *Store) detectPredecessors(issue *model.Issue) []string {
	var preds []string

	// Strategy 1: was_blocked_by entries that are closed.
	for _, wbID := range issue.WasBlockedBy {
		if contains(issue.Follows, wbID) {
			continue
		}
		if wb, err := s.ReadIssue(wbID); err == nil && wb.Status == model.StatusClosed {
			preds = append(preds, wbID)
		}
	}
	if len(preds) > 0 {
		return preds
	}

	// Strategy 2: most recently closed sibling under same parent.
	if issue.Parent == "" {
		return nil
	}
	siblings, err := s.ListIssues(FilterOptions{Parent: issue.Parent, Status: "closed"})
	if err != nil || len(siblings) == 0 {
		return nil
	}

	// Sort by closed_at descending.
	sort.Slice(siblings, func(i, j int) bool {
		return siblings[i].ClosedAt > siblings[j].ClosedAt
	})

	for _, sib := range siblings {
		if sib.ID == issue.ID || contains(issue.Follows, sib.ID) {
			continue
		}
		return []string{sib.ID}
	}

	return nil
}
