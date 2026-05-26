package cmd

import (
	"encoding/json"
	"os"

	"github.com/paivot-ai/nd/internal/format"
	"github.com/paivot-ai/nd/internal/graph"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Output AI context summary",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		all, err := s.ListIssues(store.FilterOptions{})
		if err != nil {
			return err
		}

		g := graph.Build(all)
		ready := g.Ready()
		blocked := g.Blocked()

		if jsonOut {
			data := map[string]any{
				"total":   len(all),
				"ready":   ready,
				"blocked": blocked,
				"issues":  all,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(data)
		}

		format.PrimeContext(os.Stdout, all, ready, blocked)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(primeCmd)
}
