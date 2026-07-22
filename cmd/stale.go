package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/paivot-ai/nd/internal/format"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var staleCmd = &cobra.Command{
	Use:   "stale",
	Short: "List stale issues (not updated recently)",
	RunE: func(cmd *cobra.Command, args []string) error {
		days, _ := cmd.Flags().GetInt("days")
		cutoff := time.Now().UTC().AddDate(0, 0, -days)

		s, err := store.OpenRead(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issues, err := s.ListIssues(store.FilterOptions{
			Status:        "!closed",
			UpdatedBefore: cutoff,
			Sort:          "updated",
		})
		if err != nil {
			return err
		}

		if jsonOut {
			return format.JSON(os.Stdout, issues)
		}
		if len(issues) == 0 {
			fmt.Printf("No stale issues (threshold: %d days).\n", days)
			return nil
		}
		format.Table(os.Stdout, issues)
		return nil
	},
}

func init() {
	staleCmd.Flags().Int("days", 30, "days since last update to consider stale")
	rootCmd.AddCommand(staleCmd)
}
