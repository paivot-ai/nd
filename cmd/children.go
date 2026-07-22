package cmd

import (
	"os"

	"github.com/paivot-ai/nd/internal/format"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var childrenCmd = &cobra.Command{
	Use:   "children <id>",
	Short: "List child issues of a parent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		s, err := store.OpenRead(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issues, err := s.ListIssues(store.FilterOptions{Parent: id})
		if err != nil {
			return err
		}

		if jsonOut {
			return format.JSON(os.Stdout, issues)
		}
		format.Table(os.Stdout, issues)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(childrenCmd)
}
