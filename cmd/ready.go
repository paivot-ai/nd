package cmd

import (
	"os"

	"github.com/RamXX/nd/internal/format"
	"github.com/RamXX/nd/internal/graph"
	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

var readyCmd = &cobra.Command{
	Use:   "ready",
	Short: "Show actionable issues (no blockers)",
	Long: `Show issues that are actionable: not closed, not deferred, and no open blockers.

Supports the same filter flags as 'nd list' for scoping results
(e.g., --parent to scope to an epic, --label, --priority, etc.).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Always filter out closed issues -- Ready() handles closed/deferred
		// exclusion, but pre-filtering avoids loading unnecessary data.
		opts, err := buildFilterOptions(cmd, "!closed")
		if err != nil {
			return err
		}

		// Save sort/reverse/limit, then zero them -- we need to sort and
		// limit AFTER graph filtering, not before.
		sortBy := opts.Sort
		reverse := opts.Reverse
		limit := opts.Limit
		opts.Sort = ""
		opts.Reverse = false
		opts.Limit = 0

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		// Load all issues in the vault (unfiltered) for accurate graph
		// computation -- blockers may live outside the filtered set.
		all, err := s.ListIssues(store.FilterOptions{})
		if err != nil {
			return err
		}

		g := graph.Build(all)
		ready := g.Ready()

		// Now apply the user's filters to the ready set.
		ready = filterIssues(s, ready, opts)

		store.SortIssues(ready, sortBy, reverse)

		if limit > 0 && len(ready) > limit {
			ready = ready[:limit]
		}

		if jsonOut {
			return format.JSON(os.Stdout, ready)
		}
		format.Table(os.Stdout, ready)
		return nil
	},
}

// filterIssues applies FilterOptions to an already-computed slice of issues.
// This is used by ready to apply user filters after graph-based filtering.
func filterIssues(s *store.Store, issues []*model.Issue, opts store.FilterOptions) []*model.Issue {
	var result []*model.Issue
	for _, issue := range issues {
		if s.MatchesFilter(issue, opts) {
			result = append(result, issue)
		}
	}
	return result
}

func init() {
	addFilterFlags(readyCmd)
	rootCmd.AddCommand(readyCmd)
}
