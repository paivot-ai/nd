package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/paivot-ai/nd/internal/graph"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show project statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.OpenRead(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		all, err := s.ListIssues(store.FilterOptions{})
		if err != nil {
			return err
		}

		g := graph.Build(all)
		st := g.Stats()

		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}

		fmt.Printf("Total:       %d\n", st.Total)
		fmt.Printf("Open:        %d\n", st.Open)
		fmt.Printf("In Progress: %d\n", st.InProgress)
		fmt.Printf("Blocked:     %d\n", st.Blocked)
		fmt.Printf("Deferred:    %d\n", st.Deferred)
		fmt.Printf("Closed:      %d\n", st.Closed)

		// Display any custom status counts.
		builtins := map[string]bool{
			"open": true, "in_progress": true, "blocked": true,
			"deferred": true, "closed": true,
		}
		for status, count := range st.ByStatus {
			if !builtins[status] {
				label := strings.ReplaceAll(status, "_", " ")
				if len(label) > 0 {
					label = strings.ToUpper(label[:1]) + label[1:]
				}
				fmt.Printf("%-13s%d\n", label+":", count)
			}
		}

		if len(st.ByType) > 0 {
			fmt.Println("\nBy Type:")
			for t, c := range st.ByType {
				fmt.Printf("  %-12s %d\n", t, c)
			}
		}
		if len(st.ByPriority) > 0 {
			fmt.Println("\nBy Priority:")
			for p := 0; p <= 4; p++ {
				if c, ok := st.ByPriority[p]; ok {
					fmt.Printf("  P%-11d %d\n", p, c)
				}
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
