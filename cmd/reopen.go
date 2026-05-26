package cmd

import (
	"fmt"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var reopenCmd = &cobra.Command{
	Use:   "reopen <id>",
	Short: "Reopen a closed issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.ReopenIssue(id); err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("Reopened %s\n", id)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reopenCmd)
}
