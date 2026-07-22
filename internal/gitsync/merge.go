package gitsync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paivot-ai/nd/internal/enforce"
	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/nd/internal/store"
)

// MergeCommits three-way merges two backlog commits and returns the merged
// tree hash plus human-readable notes about non-trivial resolutions.
//
// File-level rules: a path changed on one side takes that side; delete vs
// modify keeps the modification; issue files changed on both sides go through
// the field-aware issue merge; any other both-changed file keeps the local
// version with a note.
func (r *Repo) MergeCommits(base, local, remote string) (string, []string, error) {
	baseFiles, err := r.treeEntries(base)
	if err != nil {
		return "", nil, err
	}
	localFiles, err := r.treeEntries(local)
	if err != nil {
		return "", nil, err
	}
	remoteFiles, err := r.treeEntries(remote)
	if err != nil {
		return "", nil, err
	}

	union := make(map[string]bool)
	for p := range baseFiles {
		union[p] = true
	}
	for p := range localFiles {
		union[p] = true
	}
	for p := range remoteFiles {
		union[p] = true
	}

	// Read every file of every side once, batched per commit.
	baseC, err := r.readSide(base, baseFiles)
	if err != nil {
		return "", nil, err
	}
	localC, err := r.readSide(local, localFiles)
	if err != nil {
		return "", nil, err
	}
	remoteC, err := r.readSide(remote, remoteFiles)
	if err != nil {
		return "", nil, err
	}

	merged := make(map[string][]byte)
	var notes []string

	paths := make([]string, 0, len(union))
	for p := range union {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		b, inBase := baseFiles[p]
		l, inLocal := localFiles[p]
		rm, inRemote := remoteFiles[p]

		switch {
		case inLocal && inRemote && l == rm:
			merged[p] = localC[p]
		case !inBase && inLocal && !inRemote:
			merged[p] = localC[p]
		case !inBase && !inLocal && inRemote:
			merged[p] = remoteC[p]
		case !inBase && inLocal && inRemote: // added on both sides, different content
			out, ns := mergeBothChanged(p, nil, localC[p], remoteC[p])
			merged[p] = out
			notes = append(notes, ns...)
		case inBase && !inLocal && !inRemote:
			// deleted on both sides: stays deleted
		case inBase && !inLocal && inRemote:
			if rm == b {
				// local deleted, remote untouched: deletion wins
			} else {
				merged[p] = remoteC[p]
				notes = append(notes, fmt.Sprintf("%s: deleted locally but modified remotely; kept remote version", p))
			}
		case inBase && inLocal && !inRemote:
			if l == b {
				// remote deleted, local untouched: deletion wins
			} else {
				merged[p] = localC[p]
				notes = append(notes, fmt.Sprintf("%s: deleted remotely but modified locally; kept local version", p))
			}
		case inBase && l == b && rm != b:
			merged[p] = remoteC[p]
		case inBase && rm == b && l != b:
			merged[p] = localC[p]
		default: // changed on both sides
			out, ns := mergeBothChanged(p, baseC[p], localC[p], remoteC[p])
			merged[p] = out
			notes = append(notes, ns...)
		}
	}

	tree, err := r.writeTreeFromFiles(merged)
	if err != nil {
		return "", nil, err
	}
	return tree, notes, nil
}

// mergeBothChanged resolves a path modified on both sides.
func mergeBothChanged(path string, base, local, remote []byte) ([]byte, []string) {
	if strings.HasPrefix(path, "issues/") && strings.HasSuffix(path, ".md") {
		return mergeIssueContent(path, base, local, remote)
	}
	return local, []string{fmt.Sprintf("%s: changed on both sides; kept local version", path)}
}

// treeEntries returns path -> blob hash for a commit.
func (r *Repo) treeEntries(commit string) (map[string]string, error) {
	out, err := r.git(nil, "ls-tree", "-r", "-z", commit)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]string)
	if out == "" {
		return entries, nil
	}
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		// Format: <mode> SP <type> SP <hash> TAB <path>
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(rec[:tab])
		if len(meta) != 3 || meta[1] != "blob" {
			continue
		}
		entries[rec[tab+1:]] = meta[2]
	}
	return entries, nil
}

func (r *Repo) readSide(commit string, files map[string]string) (map[string][]byte, error) {
	if len(files) == 0 {
		return map[string][]byte{}, nil
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return r.readBlobs(commit, paths)
}

// writeTreeFromFiles builds a tree object from an in-memory file map by
// staging it in a temp directory with a throwaway index.
func (r *Repo) writeTreeFromFiles(files map[string][]byte) (string, error) {
	tmp, err := os.MkdirTemp("", "nd-sync-merge-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	for p, content := range files {
		dst := filepath.Join(tmp, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return "", err
		}
	}
	return r.writeTreeFromDir(tmp)
}

// canonicalSections are nd's structural body sections in canonical order.
var canonicalSections = []string{
	"## Description",
	"## Acceptance Criteria",
	"## Design",
	"## Notes",
	"## History",
	"## Links",
	"## Comments",
}

// mergeIssueContent merges one issue file three ways at the field level:
//   - frontmatter scalars: side that changed wins; both changed resolves by
//     latest updated_at (deterministic tiebreak)
//   - frontmatter lists: set merge honoring per-side adds and removals
//   - History: line union ordered by timestamp
//   - Comments: block union ordered by timestamp
//   - Notes: three-way line merge (deletions honored, additions unioned)
//   - Description / Acceptance Criteria / Design: per-section three-way with
//     latest-updated_at winner on true conflicts (recorded in History)
//   - Links: regenerated from merged frontmatter
//   - content_hash: recomputed from the merged body
func mergeIssueContent(path string, base, local, remote []byte) ([]byte, []string) {
	localIssue, lerr := store.ParseIssueMarkdown(string(local))
	remoteIssue, rerr := store.ParseIssueMarkdown(string(remote))
	if lerr != nil || rerr != nil {
		return local, []string{fmt.Sprintf("%s: unparseable during merge; kept local version", path)}
	}
	var baseIssue *model.Issue
	if len(base) > 0 {
		if bi, err := store.ParseIssueMarkdown(string(base)); err == nil {
			baseIssue = bi
		}
	}
	if baseIssue == nil {
		baseIssue = &model.Issue{} // both-added: empty base makes every field an add
	}

	// Bodies with duplicated canonical headings (authored duplicates inside
	// descriptions) are ambiguous to merge section-wise; fall back to
	// whole-body latest-wins.
	if hasDuplicateHeadings(localIssue.Body) || hasDuplicateHeadings(remoteIssue.Body) {
		winner, side := local, "local"
		if !lwwPrefersLocal(localIssue, remoteIssue, local, remote) {
			winner, side = remote, "remote"
		}
		return winner, []string{fmt.Sprintf("%s: duplicated section headings; kept %s version wholesale", path, side)}
	}

	preferLocal := lwwPrefersLocal(localIssue, remoteIssue, local, remote)
	var notes []string

	merged := &model.Issue{ID: localIssue.ID}
	merged.Title = pickScalar(baseIssue.Title, localIssue.Title, remoteIssue.Title, preferLocal)
	merged.Status = model.Status(pickScalar(string(baseIssue.Status), string(localIssue.Status), string(remoteIssue.Status), preferLocal))
	merged.Priority = model.Priority(pickInt(int(baseIssue.Priority), int(localIssue.Priority), int(remoteIssue.Priority), preferLocal))
	merged.Type = model.IssueType(pickScalar(string(baseIssue.Type), string(localIssue.Type), string(remoteIssue.Type), preferLocal))
	merged.Assignee = pickScalar(baseIssue.Assignee, localIssue.Assignee, remoteIssue.Assignee, preferLocal)
	merged.Parent = pickScalar(baseIssue.Parent, localIssue.Parent, remoteIssue.Parent, preferLocal)
	merged.DeferUntil = pickScalar(baseIssue.DeferUntil, localIssue.DeferUntil, remoteIssue.DeferUntil, preferLocal)

	merged.Labels = mergeList(baseIssue.Labels, localIssue.Labels, remoteIssue.Labels)
	merged.Blocks = mergeList(baseIssue.Blocks, localIssue.Blocks, remoteIssue.Blocks)
	merged.BlockedBy = mergeList(baseIssue.BlockedBy, localIssue.BlockedBy, remoteIssue.BlockedBy)
	merged.WasBlockedBy = mergeList(baseIssue.WasBlockedBy, localIssue.WasBlockedBy, remoteIssue.WasBlockedBy)
	merged.Related = mergeList(baseIssue.Related, localIssue.Related, remoteIssue.Related)
	merged.Follows = mergeList(baseIssue.Follows, localIssue.Follows, remoteIssue.Follows)
	merged.LedTo = mergeList(baseIssue.LedTo, localIssue.LedTo, remoteIssue.LedTo)

	merged.CreatedAt = localIssue.CreatedAt
	merged.CreatedBy = localIssue.CreatedBy
	if merged.CreatedAt.IsZero() {
		merged.CreatedAt = remoteIssue.CreatedAt
		merged.CreatedBy = remoteIssue.CreatedBy
	}
	merged.UpdatedAt = localIssue.UpdatedAt
	if remoteIssue.UpdatedAt.After(merged.UpdatedAt) {
		merged.UpdatedAt = remoteIssue.UpdatedAt
	}

	// closed_at / close_reason travel with the status decision.
	if merged.Status == model.StatusClosed {
		merged.ClosedAt, merged.CloseReason = localIssue.ClosedAt, localIssue.CloseReason
		if merged.ClosedAt == "" {
			merged.ClosedAt, merged.CloseReason = remoteIssue.ClosedAt, remoteIssue.CloseReason
		}
	}

	body, bodyNotes := mergeBody(merged, baseIssue.Body, localIssue.Body, remoteIssue.Body, preferLocal)
	for _, n := range bodyNotes {
		notes = append(notes, fmt.Sprintf("%s: %s", path, n))
	}
	merged.Body = body
	merged.ContentHash = enforce.ComputeContentHash(body)

	return []byte(store.SerializeIssueMarkdown(merged)), notes
}

// lwwPrefersLocal decides the winner for both-changed conflicts: the side
// with the later updated_at. Equal timestamps break deterministically on
// content so both clones of a divergent pair resolve identically.
func lwwPrefersLocal(localIssue, remoteIssue *model.Issue, local, remote []byte) bool {
	if localIssue.UpdatedAt.After(remoteIssue.UpdatedAt) {
		return true
	}
	if remoteIssue.UpdatedAt.After(localIssue.UpdatedAt) {
		return false
	}
	return string(local) > string(remote)
}

func pickScalar(base, local, remote string, preferLocal bool) string {
	if local == remote {
		return local
	}
	if base == local {
		return remote
	}
	if base == remote {
		return local
	}
	if preferLocal {
		return local
	}
	return remote
}

func pickInt(base, local, remote int, preferLocal bool) int {
	if local == remote {
		return local
	}
	if base == local {
		return remote
	}
	if base == remote {
		return local
	}
	if preferLocal {
		return local
	}
	return remote
}

// mergeList three-way merges an ID list with set semantics: removals on
// either side are honored, additions from both sides are unioned. Base order
// is preserved, then local additions, then remote additions.
func mergeList(base, local, remote []string) []string {
	inBase := toSet(base)
	inLocal := toSet(local)
	inRemote := toSet(remote)

	var out []string
	seen := make(map[string]bool)
	for _, id := range base {
		if inLocal[id] && inRemote[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, side := range [][]string{local, remote} {
		for _, id := range side {
			if !inBase[id] && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// hasDuplicateHeadings reports whether any canonical section heading appears
// more than once in a body.
func hasDuplicateHeadings(body string) bool {
	counts := make(map[string]int)
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		for _, h := range canonicalSections {
			if t == h {
				counts[h]++
			}
		}
	}
	for _, c := range counts {
		if c > 1 {
			return true
		}
	}
	return false
}

// splitSections splits a body into its level-2 sections, preserving order.
func splitSections(body string) (order []string, sections map[string]string) {
	sections = make(map[string]string)
	lines := strings.Split(body, "\n")
	current := ""
	var buf []string
	flush := func() {
		if current != "" {
			sections[current] = strings.TrimRight(strings.Join(buf, "\n"), "\n")
			order = append(order, current)
		}
		buf = nil
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "###") {
			flush()
			current = t
			continue
		}
		if current != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return order, sections
}

// mergeBody merges issue bodies section by section.
func mergeBody(merged *model.Issue, baseBody, localBody, remoteBody string, preferLocal bool) (string, []string) {
	_, baseSecs := splitSections(baseBody)
	localOrder, localSecs := splitSections(localBody)
	remoteOrder, remoteSecs := splitSections(remoteBody)

	// Section order: canonical first, then any extra sections as encountered.
	var order []string
	seen := make(map[string]bool)
	for _, h := range canonicalSections {
		if _, ok := localSecs[h]; ok {
			order = append(order, h)
			seen[h] = true
			continue
		}
		if _, ok := remoteSecs[h]; ok {
			order = append(order, h)
			seen[h] = true
		}
	}
	for _, src := range [][]string{localOrder, remoteOrder} {
		for _, h := range src {
			if !seen[h] {
				order = append(order, h)
				seen[h] = true
			}
		}
	}

	var notes []string
	var sb strings.Builder
	sb.WriteString("\n")
	for _, h := range order {
		b := baseSecs[h]
		l, inL := localSecs[h]
		r, inR := remoteSecs[h]
		if !inL {
			l = b
		}
		if !inR {
			r = b
		}

		var content string
		switch h {
		case "## History":
			content = mergeHistoryLines(b, l, r)
		case "## Comments":
			content = mergeCommentBlocks(b, l, r)
		case "## Links":
			content = strings.TrimRight(store.BuildLinksSection(merged), "\n")
		case "## Notes":
			content = mergeLines3(b, l, r)
		default:
			if l == r {
				content = l
			} else if b == l {
				content = r
			} else if b == r {
				content = l
			} else {
				side := "remote"
				content = r
				if preferLocal {
					side = "local"
					content = l
				}
				notes = append(notes, fmt.Sprintf("%s edited on both sides; kept %s version", h, side))
			}
		}

		sb.WriteString(h)
		sb.WriteString("\n")
		if strings.TrimSpace(content) != "" {
			sb.WriteString(strings.TrimRight(content, "\n"))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	body := sb.String()

	// Record body conflicts in History so the resolution is auditable.
	if len(notes) > 0 && strings.Contains(body, "\n## History\n") {
		var entries strings.Builder
		for _, n := range notes {
			entries.WriteString(fmt.Sprintf("- %s sync-merge: %s\n", merged.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"), n))
		}
		body = strings.Replace(body, "\n## History\n", "\n## History\n"+entries.String(), 1)
	}
	return body, notes
}

// mergeHistoryLines unions history entries from all sides ordered by their
// leading timestamp. History is append-only, so no deletion handling.
func mergeHistoryLines(base, local, remote string) string {
	var all []string
	seen := make(map[string]bool)
	for _, blob := range []string{base, local, remote} {
		for _, line := range strings.Split(blob, "\n") {
			t := strings.TrimRight(line, " ")
			if strings.TrimSpace(t) == "" || seen[t] {
				continue
			}
			seen[t] = true
			all = append(all, t)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		return historyTimestamp(all[i]) < historyTimestamp(all[j])
	})
	return strings.Join(all, "\n")
}

// historyTimestamp extracts the RFC3339 timestamp from a "- <ts> ..." history
// line for lexical ordering; lines without one sort first, keeping their
// relative order (stable sort).
func historyTimestamp(line string) string {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
	if i := strings.IndexByte(t, ' '); i > 0 {
		t = t[:i]
	}
	if len(t) >= 20 && t[4] == '-' && t[7] == '-' {
		return t
	}
	return ""
}

// mergeCommentBlocks unions "### <ts> <author>" comment blocks from all
// sides, deduplicated by content, ordered by timestamp.
func mergeCommentBlocks(base, local, remote string) string {
	var blocks []string
	seen := make(map[string]bool)
	for _, blob := range []string{base, local, remote} {
		for _, blk := range splitCommentBlocks(blob) {
			key := strings.TrimSpace(blk)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			blocks = append(blocks, strings.TrimRight(blk, "\n"))
		}
	}
	sort.SliceStable(blocks, func(i, j int) bool {
		return commentTimestamp(blocks[i]) < commentTimestamp(blocks[j])
	})
	return strings.Join(blocks, "\n\n")
}

func splitCommentBlocks(content string) []string {
	var blocks []string
	var buf []string
	flush := func() {
		if len(buf) > 0 {
			blocks = append(blocks, strings.Join(buf, "\n"))
			buf = nil
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "### ") {
			flush()
		}
		buf = append(buf, line)
	}
	flush()
	return blocks
}

func commentTimestamp(block string) string {
	first := strings.TrimSpace(strings.Split(block, "\n")[0])
	first = strings.TrimSpace(strings.TrimPrefix(first, "###"))
	if i := strings.IndexByte(first, ' '); i > 0 {
		return first[:i]
	}
	return first
}

// mergeLines3 does a three-way line merge for append-mostly free text: a base
// line survives only if both sides kept it; new lines from either side are
// appended (local first), deduplicated.
func mergeLines3(base, local, remote string) string {
	baseLines := strings.Split(base, "\n")
	localSet := toSet(strings.Split(local, "\n"))
	remoteSet := toSet(strings.Split(remote, "\n"))
	baseSet := toSet(baseLines)

	var out []string
	for _, line := range baseLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if localSet[line] && remoteSet[line] {
			out = append(out, line)
		}
	}
	seen := toSet(out)
	for _, side := range []string{local, remote} {
		for _, line := range strings.Split(side, "\n") {
			if strings.TrimSpace(line) == "" || baseSet[line] || seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
