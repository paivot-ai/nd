package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/paivot-ai/nd/internal/model"
)

// PrimeContext outputs a structured summary suitable for AI context injection.
func PrimeContext(w io.Writer, issues []*model.Issue, ready, blocked []*model.Issue) {
	fmt.Fprintln(w, "# Project Status (nd prime)")
	fmt.Fprintln(w)

	// Summary counts.
	open, inProgress, closed := 0, 0, 0
	for _, i := range issues {
		switch i.Status {
		case model.StatusOpen:
			open++
		case model.StatusInProgress:
			inProgress++
		case model.StatusClosed:
			closed++
		}
	}
	fmt.Fprintf(w, "Total: %d | Open: %d | In Progress: %d | Blocked: %d | Closed: %d\n\n",
		len(issues), open, inProgress, len(blocked), closed)

	// Ready work.
	if len(ready) > 0 {
		fmt.Fprintln(w, "## Ready (actionable)")
		for _, r := range ready {
			fmt.Fprintf(w, "- %s [%s] %s (%s)\n", r.ID, r.Status, r.Title, r.Priority.Short())
		}
		fmt.Fprintln(w)
	}

	// Blocked work.
	if len(blocked) > 0 {
		fmt.Fprintln(w, "## Blocked")
		for _, b := range blocked {
			deps := strings.Join(b.BlockedBy, ", ")
			fmt.Fprintf(w, "- %s %s (blocked by: %s)\n", b.ID, b.Title, deps)
		}
		fmt.Fprintln(w)
	}

	// In-progress work.
	fmt.Fprintln(w, "## In Progress")
	found := false
	for _, i := range issues {
		if i.Status == model.StatusInProgress {
			fmt.Fprintf(w, "- %s %s (assigned: %s)\n", i.ID, i.Title, i.Assignee)
			found = true
		}
	}
	if !found {
		fmt.Fprintln(w, "(none)")
	}
}
