package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"sort"
	"strings"

	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/paivot-ai/vlt"
	"github.com/spf13/cobra"
)

// depRecord holds a deferred dependency to wire after all issues exist.
type depRecord struct {
	issueID   string
	dependsOn string
	depType   string // "parent-child", "blocks", "discovered-from", "related", "relates-to"
}

var migrateCmd = &cobra.Command{
	Use:     "migrate",
	Aliases: []string{"import"},
	Short:   "Migrate issues from beads JSONL",
	RunE: func(cmd *cobra.Command, args []string) error {
		fromBeads, _ := cmd.Flags().GetString("from-beads")
		if fromBeads == "" {
			return fmt.Errorf("--from-beads is required")
		}
		force, _ := cmd.Flags().GetBool("force")

		dir := resolveVaultDir()

		// Auto-init vault if it doesn't exist yet.
		var s *store.Store
		if _, statErr := os.Stat(dir + "/.nd.yaml"); os.IsNotExist(statErr) {
			// Infer prefix: first try issue IDs in the JSONL, then git/dir.
			prefix := peekBeadsPrefix(fromBeads)
			source := "issue IDs"
			if prefix == "" {
				var inferSource string
				prefix, inferSource = inferPrefix()
				source = inferSource
			}
			if prefix == "" {
				return fmt.Errorf("vault not initialized and could not infer prefix; run nd init --prefix <PREFIX> first")
			}

			author := "unknown"
			if u, uErr := user.Current(); uErr == nil {
				author = u.Username
			}

			var initErr error
			s, initErr = store.Init(dir, prefix, author)
			if initErr != nil {
				return fmt.Errorf("auto-init vault: %w", initErr)
			}
			if !quiet {
				fmt.Printf("Auto-initialized vault at %s (prefix: %s, inferred from %s)\n", dir, prefix, source)
			}
		} else {
			var openErr error
			s, openErr = store.Open(dir)
			if openErr != nil {
				return openErr
			}
		}
		defer s.Close()

		f, err := os.Open(fromBeads)
		if err != nil {
			return fmt.Errorf("open %s: %w", fromBeads, err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 0, 256*1024)
		scanner.Buffer(buf, 1024*1024)

		imported, skipped := 0, 0
		var deps []depRecord
		origUpdatedAt := map[string]string{}

		// Pass 1: Create all issues and collect dependencies.
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			var raw map[string]any
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				skipped++
				continue
			}

			title, _ := raw["title"].(string)
			if title == "" {
				skipped++
				continue
			}

			// Extract fields with sensible defaults.
			desc, _ := raw["description"].(string)
			issueType := extractString(raw, "issue_type", "task")
			priority := extractInt(raw, "priority", 2)
			assignee, _ := raw["assignee"].(string)
			status, _ := raw["status"].(string)

			// Fall back to owner if assignee is empty.
			if assignee == "" {
				assignee, _ = raw["owner"].(string)
			}

			// Extract labels.
			var labels []string
			if labelsRaw, ok := raw["labels"]; ok {
				if arr, ok := labelsRaw.([]any); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							labels = append(labels, s)
						}
					}
				}
			}

			// Normalize description markdown.
			desc = normalizeMarkdown(desc)

			// Preserve original ID if present, otherwise generate a new one.
			originalID, _ := raw["id"].(string)
			var issue *createdResult
			if originalID != "" {
				issue, err = createIssueForMigrate(s, originalID, title, desc, issueType, priority, assignee, labels)
			} else {
				issue, err = createIssueForMigrate(s, "", title, desc, issueType, priority, assignee, labels)
			}
			if err != nil {
				if !quiet {
					errorf("skip %q: %v", title, err)
				}
				skipped++
				continue
			}

			// Update status if not open.
			if status != "" && status != "open" {
				// Try to parse as a valid status (built-in or custom).
				if _, parseErr := model.ParseStatusWithCustom(status, s.CustomStatuses()); parseErr == nil {
					switch status {
					case "closed":
						closedAt, _ := raw["closed_at"].(string)
						reason, _ := raw["close_reason"].(string)
						_ = s.CloseIssue(issue.ID, normalizeMarkdown(reason))
						if closedAt != "" {
							// PropertySet, not UpdateField: UpdateField would
							// touch updated_at. closed_at must be set in this
							// pass because Pass 3 sorts by it.
							_ = s.Vault().PropertySet(issue.ID, "closed_at", closedAt)
						}
					default:
						_ = s.UpdateField(issue.ID, "status", status)
					}
				} else {
					// Fall back to built-in mapping for unrecognized statuses.
					switch status {
					case "in_progress":
						_ = s.UpdateField(issue.ID, "status", "in_progress")
					case "blocked":
						_ = s.UpdateField(issue.ID, "status", "blocked")
					case "deferred":
						_ = s.UpdateField(issue.ID, "status", "deferred")
					}
				}
			}

			// Preserve created_at now; updated_at is restored at the end of
			// the import because every later pass (notes, sections, dependency
			// wiring, epic promotion) touches it.
			if createdAt, ok := raw["created_at"].(string); ok && createdAt != "" {
				_ = s.Vault().PropertySet(issue.ID, "created_at", createdAt)
			}
			if updatedAt, ok := raw["updated_at"].(string); ok && updatedAt != "" {
				origUpdatedAt[issue.ID] = updatedAt
			}

			// Import notes (normalized).
			if notes, ok := raw["notes"].(string); ok && notes != "" {
				_ = s.AppendNotes(issue.ID, normalizeMarkdown(notes))
			}

			// Import design and acceptance criteria (normalized). These patch
			// the body directly, so the content hash is refreshed after.
			sectionPatched := false
			if design, ok := raw["design"].(string); ok && design != "" {
				_ = s.Vault().Patch(issue.ID, vlt.PatchOptions{
					Heading: "## Design",
					Content: normalizeMarkdown(design) + "\n",
				})
				sectionPatched = true
			}

			if ac, ok := raw["acceptance_criteria"].(string); ok && ac != "" {
				_ = s.Vault().Patch(issue.ID, vlt.PatchOptions{
					Heading: "## Acceptance Criteria",
					Content: normalizeMarkdown(ac) + "\n",
				})
				sectionPatched = true
			}

			if sectionPatched {
				_ = s.RecomputeContentHash(issue.ID)
			}

			// Import defer_until.
			if deferUntil, ok := raw["defer_until"].(string); ok && deferUntil != "" {
				_ = s.UpdateField(issue.ID, "defer_until", deferUntil)
			}

			// Collect dependency records for Pass 2.
			if depsRaw, ok := raw["dependencies"]; ok {
				if arr, ok := depsRaw.([]any); ok {
					for _, d := range arr {
						dm, ok := d.(map[string]any)
						if !ok {
							continue
						}
						depIssueID, _ := dm["issue_id"].(string)
						dependsOnID, _ := dm["depends_on_id"].(string)
						depType, _ := dm["type"].(string)
						if depIssueID != "" && dependsOnID != "" && depType != "" {
							deps = append(deps, depRecord{
								issueID:   depIssueID,
								dependsOn: dependsOnID,
								depType:   depType,
							})
						}
					}
				}
			}

			imported++
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		// Gate: skip passes 2-3 if nothing was imported (idempotent re-run).
		if imported == 0 && !force {
			fmt.Printf("Migrated %d issues (%d skipped)\n", imported, skipped)
			fmt.Println("All issues already exist. Use --force to re-wire dependencies.")
			return nil
		}

		// Infer parent-child from dotted IDs (e.g. TM-abc.3 -> parent TM-abc).
		dottedRe := regexp.MustCompile(`^(.+)\.\d+$`)
		// Build a set of explicit parent-child pairs to avoid duplicates.
		explicitPC := map[[2]string]bool{}
		for _, d := range deps {
			if d.depType == "parent-child" {
				explicitPC[[2]string{d.issueID, d.dependsOn}] = true
			}
		}
		// Scan all created issue IDs for dotted pattern.
		allIssues, _ := s.ListIssues(store.FilterOptions{Status: "all"})
		for _, issue := range allIssues {
			m := dottedRe.FindStringSubmatch(issue.ID)
			if m == nil {
				continue
			}
			baseID := m[1]
			pair := [2]string{issue.ID, baseID}
			if explicitPC[pair] {
				continue
			}
			if s.IssueExists(baseID) {
				deps = append(deps, depRecord{
					issueID:   issue.ID,
					dependsOn: baseID,
					depType:   "parent-child",
				})
			}
		}

		// Infer parent-child from text references in the JSONL data.
		// Reparse JSONL to build epic->mentions and orphan->mentions maps.
		alreadyParented := map[string]bool{}
		for _, d := range deps {
			if d.depType == "parent-child" {
				alreadyParented[d.issueID] = true
			}
		}
		epicIDs := map[string]bool{}
		epicTexts := map[string]string{} // epicID -> concatenated text fields
		allIDsSet := map[string]bool{}
		issueTexts := map[string]string{} // non-epic issueID -> concatenated text
		issueTypes := map[string]string{}
		{
			rf, _ := os.Open(fromBeads)
			if rf != nil {
				sc2 := bufio.NewScanner(rf)
				buf2 := make([]byte, 0, 256*1024)
				sc2.Buffer(buf2, 1024*1024)
				for sc2.Scan() {
					var raw map[string]any
					if json.Unmarshal([]byte(sc2.Text()), &raw) != nil {
						continue
					}
					iid, _ := raw["id"].(string)
					if iid == "" {
						continue
					}
					allIDsSet[iid] = true
					itype := extractString(raw, "issue_type", "task")
					issueTypes[iid] = itype
					var parts []string
					for _, f := range []string{"description", "close_reason", "notes", "acceptance_criteria", "design"} {
						if v, ok := raw[f].(string); ok {
							parts = append(parts, v)
						}
					}
					combined := strings.Join(parts, " ")
					if itype == "epic" {
						epicIDs[iid] = true
						epicTexts[iid] = combined
					} else {
						issueTexts[iid] = combined
					}
				}
				rf.Close()
			}
		}
		// For each non-epic orphan, check if exactly one epic mentions it.
		idRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(s.Prefix()) + `-[a-z0-9]+(?:\.\d+)?\b`)
		orphanToEpic := map[string]string{}
		for eid, text := range epicTexts {
			for _, ref := range idRe.FindAllString(text, -1) {
				if ref == eid || !allIDsSet[ref] || epicIDs[ref] || alreadyParented[ref] {
					continue
				}
				if _, seen := orphanToEpic[ref]; seen {
					orphanToEpic[ref] = "" // ambiguous: multiple epics mention it
				} else {
					orphanToEpic[ref] = eid
				}
			}
		}
		// Also check: orphan text mentions exactly one epic.
		for oid, text := range issueTexts {
			if alreadyParented[oid] {
				continue
			}
			if _, already := orphanToEpic[oid]; already {
				continue
			}
			var epicRefs []string
			for _, ref := range idRe.FindAllString(text, -1) {
				if epicIDs[ref] && ref != oid {
					epicRefs = append(epicRefs, ref)
				}
			}
			// Deduplicate.
			seen := map[string]bool{}
			var unique []string
			for _, r := range epicRefs {
				if !seen[r] {
					seen[r] = true
					unique = append(unique, r)
				}
			}
			if len(unique) == 1 {
				orphanToEpic[oid] = unique[0]
			}
		}
		// Add unambiguous text-inferred parent-child deps.
		textInferred := 0
		for childID, parentID := range orphanToEpic {
			if parentID == "" || !s.IssueExists(childID) || !s.IssueExists(parentID) {
				continue
			}
			deps = append(deps, depRecord{
				issueID:   childID,
				dependsOn: parentID,
				depType:   "parent-child",
			})
			textInferred++
		}

		// Pass 2: Wire dependencies now that all issues exist.
		parentChild, blocks, related := 0, 0, 0
		parentIDs := map[string]bool{}

		for _, d := range deps {
			if !s.IssueExists(d.issueID) || !s.IssueExists(d.dependsOn) {
				continue
			}
			switch d.depType {
			case "parent-child":
				if err := s.SetParent(d.issueID, d.dependsOn); err == nil {
					parentChild++
					parentIDs[d.dependsOn] = true
				}
			case "blocks":
				if err := s.AddDependency(d.issueID, d.dependsOn); err == nil {
					blocks++
				}
			case "discovered-from", "related", "relates-to":
				if err := s.AddRelated(d.issueID, d.dependsOn); err == nil {
					related++
				}
			}
		}

		// Pass 3: Infer follows/led_to chains from closed_at timestamps.
		followsWired := 0

		// 3a: Sibling chains under shared parents.
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

		// 3b: Top-level orphan chains via related links.
		// Build a map of related pairs among closed, parentless issues.
		type relatedPair struct{ a, b string }
		var relatedPairs []relatedPair
		closedOrphans := map[string]*model.Issue{}
		{
			orphans, _ := s.ListIssues(store.FilterOptions{NoParent: true, Status: "closed"})
			for _, o := range orphans {
				closedOrphans[o.ID] = o
			}
			seen := map[relatedPair]bool{}
			for _, o := range orphans {
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

		// 3c: Epic-to-epic chains by last-child closed_at.
		type epicClose struct {
			id       string
			closedAt string
		}
		var epicCloses []epicClose
		for pid := range parentIDs {
			ep, err := s.ReadIssue(pid)
			if err != nil || ep.Status.String() != "closed" {
				continue
			}
			children, _ := s.ListIssues(store.FilterOptions{Parent: pid, Status: "closed"})
			if len(children) == 0 {
				if ep.ClosedAt != "" {
					epicCloses = append(epicCloses, epicClose{pid, ep.ClosedAt})
				}
				continue
			}
			lastClose := ""
			for _, c := range children {
				if c.ClosedAt > lastClose {
					lastClose = c.ClosedAt
				}
			}
			if lastClose != "" {
				epicCloses = append(epicCloses, epicClose{pid, lastClose})
			}
		}
		if len(epicCloses) > 1 {
			sort.Slice(epicCloses, func(i, j int) bool {
				return epicCloses[i].closedAt < epicCloses[j].closedAt
			})
			for i := 1; i < len(epicCloses); i++ {
				if err := s.AddFollows(epicCloses[i].id, epicCloses[i-1].id); err == nil {
					followsWired++
				}
			}
		}

		// Promote parents to epic if not already.
		promoted := 0
		for pid := range parentIDs {
			issue, err := s.ReadIssue(pid)
			if err != nil {
				continue
			}
			if issue.Type != "epic" {
				if err := s.UpdateField(pid, "type", "epic"); err == nil {
					promoted++
				}
			}
		}

		// Restore original updated_at timestamps last: every pass above
		// touches updated_at and would clobber the imported values.
		for id, ts := range origUpdatedAt {
			_ = s.Vault().PropertySet(id, "updated_at", ts)
		}

		fmt.Printf("Migrated %d issues (%d skipped)\n", imported, skipped)
		if parentChild+blocks+related > 0 {
			fmt.Printf("  Wired: %d parent-child, %d blocks, %d related\n", parentChild, blocks, related)
		}
		if textInferred > 0 {
			fmt.Printf("  Text-inferred: %d parent-child from epic/issue cross-references\n", textInferred)
		}
		if promoted > 0 {
			fmt.Printf("  Promoted %d issues to epic\n", promoted)
		}
		if followsWired > 0 {
			fmt.Printf("  Trajectory: %d follows/led_to links inferred from timestamps\n", followsWired)
		}
		return nil
	},
}

// createIssueForMigrate wraps the store create methods, returning a minimal result.
type createdResult struct {
	ID string
}

func createIssueForMigrate(s *store.Store, id, title, desc, issueType string, priority int, assignee string, labels []string) (*createdResult, error) {
	if id != "" {
		issue, err := s.CreateIssueWithID(id, title, desc, issueType, priority, assignee, labels, "")
		if err != nil {
			return nil, err
		}
		return &createdResult{ID: issue.ID}, nil
	}
	issue, err := s.CreateIssue(title, desc, issueType, priority, assignee, labels, "")
	if err != nil {
		return nil, err
	}
	return &createdResult{ID: issue.ID}, nil
}

// normalizeMarkdown fixes CommonMark rendering issues and escapes false Obsidian tags:
// 1. Headings without blank line after: ## Foo\ncontent -> ## Foo\n\ncontent
// 2. Tables without blank line before: text\n| col | -> text\n\n| col |
// 3. Code-like #patterns (hex colors, preprocessor, CSS selectors) wrapped in backticks
func normalizeMarkdown(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	var out []string
	for i, line := range lines {
		out = append(out, line)
		if i >= len(lines)-1 {
			continue
		}
		next := lines[i+1]
		trimmed := strings.TrimSpace(line)
		nextTrimmed := strings.TrimSpace(next)

		// After a heading, ensure blank line if next line is not blank and not a heading.
		if strings.HasPrefix(trimmed, "#") && nextTrimmed != "" && !strings.HasPrefix(nextTrimmed, "#") {
			out = append(out, "")
		}

		// Before a table row, ensure blank line if current line is not blank and not a table row.
		if strings.HasPrefix(nextTrimmed, "|") && trimmed != "" && !strings.HasPrefix(trimmed, "|") && !strings.HasPrefix(trimmed, "#") {
			out = append(out, "")
		}
	}
	result := strings.Join(out, "\n")
	return escapeFalseTags(result)
}

// falseTagPattern matches any #word pattern that Obsidian would interpret as a tag.
// This includes hex colors (#22c55e), preprocessor directives (#if), CSS selectors
// (section#proof), hash routes (#/accounts), and any other inline #<text> that is
// not a markdown heading.
var falseTagPattern = regexp.MustCompile(`#/?[a-zA-Z0-9][a-zA-Z0-9_/:?&=.-]*`)

// escapeFalseTags wraps code-like # patterns in backticks so Obsidian
// does not interpret them as tags. Skips content already inside backticks.
func escapeFalseTags(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Skip markdown headings.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && (len(trimmed) < 2 || trimmed[1] == ' ' || trimmed[1] == '#') {
			continue
		}
		lines[i] = escapeLinefalseTags(line)
	}
	return strings.Join(lines, "\n")
}

// escapeLinefalseTags processes a single line, replacing false tags with
// backtick-wrapped versions while preserving content already in backticks.
func escapeLinefalseTags(line string) string {
	// Split line into segments: inside backticks vs outside.
	// Only process segments outside backticks.
	var result strings.Builder
	rest := line
	for {
		tick := strings.Index(rest, "`")
		if tick < 0 {
			result.WriteString(escapeSegment(rest))
			break
		}
		result.WriteString(escapeSegment(rest[:tick]))
		rest = rest[tick:]

		// Find closing backtick.
		close := strings.Index(rest[1:], "`")
		if close < 0 {
			result.WriteString(rest)
			break
		}
		end := close + 2
		result.WriteString(rest[:end])
		rest = rest[end:]
	}
	return result.String()
}

// escapeSegment escapes false tags in a text segment that is NOT inside backticks.
func escapeSegment(s string) string {
	return falseTagPattern.ReplaceAllStringFunc(s, wrapInBackticks)
}

func wrapInBackticks(s string) string {
	return "`" + s + "`"
}

func extractString(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func extractInt(m map[string]any, key string, fallback int) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case string:
		v = strings.TrimPrefix(strings.ToUpper(v), "P")
		for i := 0; i <= 4; i++ {
			if v == fmt.Sprintf("%d", i) {
				return i
			}
		}
	}
	return fallback
}

func init() {
	migrateCmd.Flags().String("from-beads", "", "path to beads JSONL file")
	migrateCmd.Flags().Bool("force", false, "re-wire dependencies even if no new issues were imported")
	rootCmd.AddCommand(migrateCmd)
}
