package cmd

import (
	"os"

	"github.com/RamXX/nd/internal/format"
	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

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

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issues, err := s.ListIssues(opts)
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
	addFilterFlags(listCmd)
	listCmd.Flags().Bool("all", false, "show all issues including closed")
	rootCmd.AddCommand(listCmd)
}
