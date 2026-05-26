package cmd

import (
	"fmt"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <id> [id...]",
	Short: "Delete one or more issues",
	Long:  "Delete issues, cleaning up all dependency references. Soft-delete (to .trash/) by default.",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		permanent, _ := cmd.Flags().GetBool("permanent")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		var errors []string
		for _, id := range args {
			if dryRun {
				issue, err := s.ReadIssue(id)
				if err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", id, err))
					continue
				}
				fmt.Printf("Would delete %s: %s\n", id, issue.Title)
				if len(issue.BlockedBy) > 0 {
					fmt.Printf("  Would clean blocked_by: %v\n", issue.BlockedBy)
				}
				if len(issue.Blocks) > 0 {
					fmt.Printf("  Would clean blocks: %v\n", issue.Blocks)
				}
				continue
			}

			modified, err := s.DeleteIssue(id, permanent)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", id, err))
				continue
			}
			if !quiet {
				mode := "Deleted"
				if !permanent {
					mode = "Trashed"
				}
				fmt.Printf("%s %s\n", mode, id)
				for _, m := range modified {
					fmt.Printf("  Updated deps: %s\n", m)
				}
			}
		}

		if len(errors) > 0 {
			for _, e := range errors {
				errorf("%s", e)
			}
			return fmt.Errorf("%d issue(s) failed to delete", len(errors))
		}
		return nil
	},
}

func init() {
	deleteCmd.Flags().Bool("permanent", false, "permanently delete (skip trash)")
	deleteCmd.Flags().Bool("dry-run", false, "show what would be deleted")
	rootCmd.AddCommand(deleteCmd)
}
