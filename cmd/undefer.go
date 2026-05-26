package cmd

import (
	"fmt"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var undeferCmd = &cobra.Command{
	Use:   "undefer <id>",
	Short: "Restore a deferred issue to its configured resume status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.UnDeferIssue(id); err != nil {
			return err
		}

		if !quiet {
			fmt.Printf("Undeferred %s\n", id)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(undeferCmd)
}
