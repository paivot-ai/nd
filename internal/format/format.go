package format

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/nd/internal/store"
	"github.com/RamXX/nd/internal/ui"
)

// FormatIssueLine renders a single issue as a one-line string suitable for
// Table or Tree output. Closed issues are rendered with RenderClosedLine.
func FormatIssueLine(issue *model.Issue) string {
	title := issue.Title
	if len(title) > 60 {
		title = title[:57] + "..."
	}

	status := string(issue.Status)
	isClosed := issue.Status == model.StatusClosed

	if isClosed {
		line := fmt.Sprintf("%s %s [P%d] [%s] - %s",
			ui.StatusIconClosed, issue.ID, issue.Priority, issue.Type, title)
		return ui.RenderClosedLine(line)
	}

	var parts []string
	parts = append(parts, ui.RenderStatusIcon(status))
	parts = append(parts, ui.RenderID(issue.ID))
	parts = append(parts, fmt.Sprintf("[%s]", ui.RenderPriority(int(issue.Priority))))
	parts = append(parts, fmt.Sprintf("[%s]", ui.RenderType(string(issue.Type))))
	if issue.Assignee != "" {
		parts = append(parts, fmt.Sprintf("@%s", issue.Assignee))
	}
	if len(issue.Labels) > 0 {
		parts = append(parts, fmt.Sprintf("[%s]", strings.Join(issue.Labels, ", ")))
	}
	parts = append(parts, fmt.Sprintf("- %s", title))

	return strings.Join(parts, " ")
}

// Table renders a compact issue list with status icons, colors, and bd-style formatting.
// Format: STATUS_ICON ID [PRIORITY] [TYPE] @ASSIGNEE [LABELS] - TITLE
func Table(w io.Writer, issues []*model.Issue) {
	if len(issues) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	for _, issue := range issues {
		fmt.Fprintln(w, FormatIssueLine(issue))
	}
	fmt.Fprintf(w, "\n%d issue(s)\n", len(issues))
}

// Tree renders issues grouped by parent with tree connectors (├──/└──).
// contextIDs marks parents that were fetched only for display context (not in
// the original filter result); they are rendered muted and excluded from count.
// sortBy and reverse control ordering of issues within each group.
func Tree(w io.Writer, issues []*model.Issue, contextIDs map[string]bool, sortBy string, reverse bool) {
	if len(issues) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	// Build a lookup and a parent->children map.
	issueMap := make(map[string]*model.Issue, len(issues))
	childrenOf := make(map[string][]*model.Issue) // parentID -> children
	var unparented []*model.Issue
	var topLevel []*model.Issue

	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}

	for _, issue := range issues {
		if issue.Parent == "" {
			// No parent at all.
			unparented = append(unparented, issue)
		} else if _, parentInSlice := issueMap[issue.Parent]; parentInSlice {
			// Parent is in the slice -- this issue is a child.
			childrenOf[issue.Parent] = append(childrenOf[issue.Parent], issue)
		} else {
			// Parent not in the slice (and not fetched) -- treat as unparented.
			unparented = append(unparented, issue)
		}
	}

	// Identify top-level parents: issues that have children (or are context-only
	// parents) and are not themselves children of another issue in the slice.
	for _, issue := range issues {
		if len(childrenOf[issue.ID]) > 0 || contextIDs[issue.ID] {
			// Check this issue is not a child of another issue in the slice.
			if issue.Parent == "" || issueMap[issue.Parent] == nil {
				topLevel = append(topLevel, issue)
			}
			// If it IS a child, it will be rendered under its parent already.
		}
	}

	// Remove top-level parents from unparented (they are rendered separately).
	topLevelSet := make(map[string]bool, len(topLevel))
	for _, issue := range topLevel {
		topLevelSet[issue.ID] = true
	}
	filtered := unparented[:0]
	for _, issue := range unparented {
		if !topLevelSet[issue.ID] {
			filtered = append(filtered, issue)
		}
	}
	unparented = filtered

	// Sort groups.
	store.SortIssues(topLevel, sortBy, reverse)
	store.SortIssues(unparented, sortBy, reverse)

	// Count only non-context issues.
	issueCount := 0
	for _, issue := range issues {
		if !contextIDs[issue.ID] {
			issueCount++
		}
	}

	// Render top-level parents and their children.
	for _, parent := range topLevel {
		renderTreeNode(w, parent, childrenOf, contextIDs, sortBy, reverse, "")
	}

	// Render [Unparented] section if there are unparented issues.
	if len(unparented) > 0 {
		fmt.Fprintln(w, ui.RenderMuted("[Unparented]"))
		for i, issue := range unparented {
			connector := "├── "
			if i == len(unparented)-1 {
				connector = "└── "
			}
			fmt.Fprintln(w, connector+FormatIssueLine(issue))
		}
	}

	fmt.Fprintf(w, "\n%d issue(s)\n", issueCount)
}

// renderTreeNode renders a parent issue and its children recursively.
func renderTreeNode(w io.Writer, issue *model.Issue, childrenOf map[string][]*model.Issue, contextIDs map[string]bool, sortBy string, reverse bool, prefix string) {
	// Render the issue itself.
	line := FormatIssueLine(issue)
	if contextIDs[issue.ID] {
		line = ui.RenderClosedLine(line)
	}
	fmt.Fprintln(w, prefix+line)

	children := childrenOf[issue.ID]
	if len(children) == 0 {
		return
	}

	// Sort children within group.
	store.SortIssues(children, sortBy, reverse)

	for i, child := range children {
		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(children)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		childLine := FormatIssueLine(child)
		if contextIDs[child.ID] {
			childLine = ui.RenderClosedLine(childLine)
		}
		fmt.Fprintln(w, prefix+connector+childLine)

		// Recurse for deeper nesting.
		if len(childrenOf[child.ID]) > 0 {
			renderTreeNode_children(w, child.ID, childrenOf, contextIDs, sortBy, reverse, childPrefix)
		}
	}
}

// renderTreeNode_children renders grandchildren+ at the correct indent level.
func renderTreeNode_children(w io.Writer, parentID string, childrenOf map[string][]*model.Issue, contextIDs map[string]bool, sortBy string, reverse bool, prefix string) {
	children := childrenOf[parentID]
	store.SortIssues(children, sortBy, reverse)

	for i, child := range children {
		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(children)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		childLine := FormatIssueLine(child)
		if contextIDs[child.ID] {
			childLine = ui.RenderClosedLine(childLine)
		}
		fmt.Fprintln(w, prefix+connector+childLine)

		if len(childrenOf[child.ID]) > 0 {
			renderTreeNode_children(w, child.ID, childrenOf, contextIDs, sortBy, reverse, childPrefix)
		}
	}
}

// Detail renders a single issue with colored output and markdown body.
func Detail(w io.Writer, issue *model.Issue) {
	status := string(issue.Status)

	// Header: STATUS_ICON ID . TITLE [PRIORITY . STATUS]
	fmt.Fprintf(w, "%s %s %s %s [%s %s %s]\n",
		ui.RenderStatusIcon(status),
		ui.RenderID(issue.ID),
		ui.RenderMuted("."),
		ui.RenderBold(issue.Title),
		ui.RenderPriority(int(issue.Priority)),
		ui.RenderMuted("."),
		ui.RenderStatus(status),
	)

	// Metadata line 1: Owner . Type
	var meta1 []string
	if issue.Assignee != "" {
		meta1 = append(meta1, fmt.Sprintf("%s %s", ui.RenderAccent("Owner:"), issue.Assignee))
	}
	meta1 = append(meta1, fmt.Sprintf("%s %s", ui.RenderAccent("Type:"), ui.RenderType(string(issue.Type))))
	fmt.Fprintln(w, strings.Join(meta1, fmt.Sprintf(" %s ", ui.RenderMuted("."))))

	// Metadata line 2: Created . Updated
	fmt.Fprintf(w, "%s %s %s %s %s\n",
		ui.RenderAccent("Created:"),
		issue.CreatedAt.Format("2006-01-02 15:04"),
		ui.RenderMuted("."),
		ui.RenderAccent("Updated:"),
		issue.UpdatedAt.Format("2006-01-02 15:04"),
	)

	if issue.CreatedBy != "" {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Author:"), issue.CreatedBy)
	}
	if len(issue.Labels) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Labels:"), strings.Join(issue.Labels, ", "))
	}
	if issue.Parent != "" {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Parent:"), issue.Parent)
	}
	if len(issue.Blocks) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Blocks:"), strings.Join(issue.Blocks, ", "))
	}
	if len(issue.BlockedBy) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Blocked by:"), strings.Join(issue.BlockedBy, ", "))
	}
	if len(issue.Related) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Related:"), strings.Join(issue.Related, ", "))
	}
	if issue.ClosedAt != "" {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Closed:"), issue.ClosedAt)
	}
	if issue.CloseReason != "" {
		fmt.Fprintf(w, "%s %s\n", ui.RenderAccent("Reason:"), issue.CloseReason)
	}

	if issue.Body != "" {
		fmt.Fprintln(w)
		fmt.Fprint(w, ui.RenderMarkdown(issue.Body))
	}
}

// JSON outputs issues as JSON.
func JSON(w io.Writer, issues []*model.Issue) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(issues)
}

// JSONSingle outputs a single issue as JSON.
func JSONSingle(w io.Writer, issue *model.Issue) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(issue)
}

// Short renders a one-line summary of an issue.
func Short(w io.Writer, issue *model.Issue) {
	fmt.Fprintf(w, "%s %s [%s] %s (%s)\n",
		ui.RenderStatusIcon(string(issue.Status)),
		issue.ID,
		ui.RenderStatus(string(issue.Status)),
		issue.Title,
		ui.RenderPriority(int(issue.Priority)),
	)
}
