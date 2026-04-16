package cmd

import (
	"fmt"
	"os"

	"github.com/RamXX/nd/internal/format"
	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

var validGroupByValues = []string{"parent"}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues",
	RunE: func(cmd *cobra.Command, args []string) error {
		showAll, _ := cmd.Flags().GetBool("all")

		// Default: show non-closed issues (matching bd behavior).
		// Use --status=all to see everything, --status=closed for closed only.
		defaultStatus := "!closed"
		if showAll {
			defaultStatus = "all"
		}

		opts, err := buildFilterOptions(cmd, defaultStatus)
		if err != nil {
			return err
		}

		// --all without explicit --limit removes the default cap.
		if showAll && !cmd.Flags().Changed("limit") {
			opts.Limit = 0
		}

		// list defaults to 50 when limit not explicitly set.
		if !showAll && !cmd.Flags().Changed("limit") {
			opts.Limit = 50
		}

		groupBy, _ := cmd.Flags().GetString("group-by")
		if groupBy != "" && !isValidGroupBy(groupBy) {
			return fmt.Errorf("invalid --group-by value %q: valid values are: parent", groupBy)
		}

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issues, err := s.ListIssues(opts)
		if err != nil {
			return err
		}

		// --json always emits a flat array, ignoring --group-by.
		if jsonOut {
			return format.JSON(os.Stdout, issues)
		}

		if groupBy == "parent" {
			// Collect parent IDs that are referenced but not in the filtered result set.
			issueIndex := make(map[string]bool, len(issues))
			for _, issue := range issues {
				issueIndex[issue.ID] = true
			}

			contextIDs := make(map[string]bool)
			for _, issue := range issues {
				if issue.Parent != "" && !issueIndex[issue.Parent] {
					contextIDs[issue.Parent] = true
				}
			}

			// Fetch context-only parents.
			for parentID := range contextIDs {
				parent, err := s.ReadIssue(parentID)
				if err != nil {
					continue // parent may have been deleted
				}
				issues = append(issues, parent)
			}

			sortBy, _ := cmd.Flags().GetString("sort")
			reverse, _ := cmd.Flags().GetBool("reverse")
			format.Tree(os.Stdout, issues, contextIDs, sortBy, reverse)
			return nil
		}

		format.Table(os.Stdout, issues)
		return nil
	},
}

func isValidGroupBy(s string) bool {
	for _, v := range validGroupByValues {
		if s == v {
			return true
		}
	}
	return false
}

func init() {
	addFilterFlags(listCmd)
	listCmd.Flags().Bool("all", false, "show all issues including closed")
	rootCmd.AddCommand(listCmd)
}
