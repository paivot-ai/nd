package cmd

import (
	"fmt"
	"os"

	"github.com/paivot-ai/nd/internal/format"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show issue detail",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		short, _ := cmd.Flags().GetBool("short")

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issue, err := s.ReadIssue(id)
		if err != nil {
			return fmt.Errorf("issue %s not found: %w", id, err)
		}

		if jsonOut {
			return format.JSONSingle(os.Stdout, issue)
		}
		if short {
			format.Short(os.Stdout, issue)
			return nil
		}
		format.Detail(os.Stdout, issue)
		return nil
	},
}

func init() {
	showCmd.Flags().Bool("short", false, "one-line summary")
	rootCmd.AddCommand(showCmd)
}
