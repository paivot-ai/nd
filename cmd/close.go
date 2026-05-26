package cmd

import (
	"fmt"
	"os"

	"github.com/paivot-ai/nd/internal/format"
	"github.com/paivot-ai/nd/internal/graph"
	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var closeCmd = &cobra.Command{
	Use:   "close <id> [id...]",
	Short: "Close one or more issues",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		var errors []string
		for _, id := range args {
			if err := s.CloseIssue(id, reason); err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", id, err))
				continue
			}
			if !quiet {
				fmt.Printf("Closed %s\n", id)
			}

			// Cascade: remove this issue from dependents' blocked_by lists.
			unblocked, err := s.ResolveDependentsOf(id)
			if err != nil {
				errorf("cascade %s: %v", id, err)
			}
			if !quiet {
				for _, uid := range unblocked {
					fmt.Printf("  Unblocked %s (was blocked by %s)\n", uid, id)
				}
			}
		}

		if len(errors) > 0 {
			for _, e := range errors {
				errorf("%s", e)
			}
			return fmt.Errorf("%d issue(s) failed to close", len(errors))
		}

		suggestNext, _ := cmd.Flags().GetBool("suggest-next")
		if suggestNext && !quiet {
			all, err := s.ListIssues(store.FilterOptions{Status: "!closed"})
			if err == nil {
				g := graph.Build(all)
				ready := g.Ready()
				if len(ready) > 0 {
					fmt.Fprintf(os.Stderr, "\nNext ready issue:\n")
					format.Short(os.Stderr, ready[0])
				}
			}
		}

		startNext, _ := cmd.Flags().GetString("start")
		if startNext != "" {
			if err := s.UpdateStatus(startNext, model.StatusInProgress); err != nil {
				return fmt.Errorf("start %s: %w", startNext, err)
			}
			if !quiet {
				fmt.Printf("Started %s\n", startNext)
			}
		}

		return nil
	},
}

func init() {
	closeCmd.Flags().String("reason", "", "close reason")
	closeCmd.Flags().Bool("force", false, "close even if blocked")
	closeCmd.Flags().Bool("suggest-next", false, "show top ready issue after closing")
	closeCmd.Flags().String("start", "", "start the next issue after closing")
	rootCmd.AddCommand(closeCmd)
}
