package cmd

import (
	"fmt"
	"strings"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/paivot-ai/vlt"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search across issues",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")

		s, err := store.OpenRead(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		results, err := s.Vault().SearchWithContext(vlt.SearchOptions{
			Query:    query,
			Path:     "issues",
			ContextN: 2,
		})
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Println("No matches.")
			return nil
		}

		for _, m := range results {
			fmt.Printf("%s:%d\n", m.File, m.Line)
			for _, line := range m.Context {
				fmt.Printf("  %s\n", line)
			}
			fmt.Println()
		}
		fmt.Printf("%d match(es)\n", len(results))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
