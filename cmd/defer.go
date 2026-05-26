package cmd

import (
	"fmt"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var deferCmd = &cobra.Command{
	Use:   "defer <id>",
	Short: "Defer an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		until, _ := cmd.Flags().GetString("until")

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.DeferIssue(id, until); err != nil {
			return err
		}

		if !quiet {
			if until != "" {
				fmt.Printf("Deferred %s until %s\n", id, until)
			} else {
				fmt.Printf("Deferred %s\n", id)
			}
		}
		return nil
	},
}

func init() {
	deferCmd.Flags().String("until", "", "defer until date (YYYY-MM-DD)")
	rootCmd.AddCommand(deferCmd)
}
