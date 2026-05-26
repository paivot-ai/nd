package format

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/nd/internal/ui"
)

// Table renders a compact issue list with status icons, colors, and bd-style formatting.
// Format: STATUS_ICON ID [PRIORITY] [TYPE] @ASSIGNEE [LABELS] - TITLE
func Table(w io.Writer, issues []*model.Issue) {
	if len(issues) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	for _, issue := range issues {
		title := issue.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}

		status := string(issue.Status)
		isClosed := issue.Status == model.StatusClosed

		if isClosed {
			// Entire line muted for closed issues.
			line := fmt.Sprintf("%s %s [P%d] [%s] - %s",
				ui.StatusIconClosed, issue.ID, issue.Priority, issue.Type, title)
			fmt.Fprintln(w, ui.RenderClosedLine(line))
			continue
		}

		// Build colored line.
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

		fmt.Fprintln(w, strings.Join(parts, " "))
	}
	fmt.Fprintf(w, "\n%d issue(s)\n", len(issues))
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
