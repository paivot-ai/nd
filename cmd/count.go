package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var countCmd = &cobra.Command{
	Use:   "count",
	Short: "Count issues grouped by a field",
	RunE: func(cmd *cobra.Command, args []string) error {
		by, _ := cmd.Flags().GetString("by")
		status, _ := cmd.Flags().GetString("status")

		if !cmd.Flags().Changed("status") {
			status = "!closed"
		}

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issues, err := s.ListIssues(store.FilterOptions{Status: status})
		if err != nil {
			return err
		}

		counts := make(map[string]int)
		for _, issue := range issues {
			var key string
			switch strings.ToLower(by) {
			case "status":
				key = string(issue.Status)
			case "type":
				key = string(issue.Type)
			case "priority":
				key = issue.Priority.Short()
			case "assignee":
				key = issue.Assignee
				if key == "" {
					key = "(unassigned)"
				}
			case "label":
				if len(issue.Labels) == 0 {
					counts["(unlabeled)"]++
					continue
				}
				for _, l := range issue.Labels {
					counts[l]++
				}
				continue
			default:
				return fmt.Errorf("invalid --by value %q: use status, type, priority, assignee, or label", by)
			}
			counts[key]++
		}

		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(counts)
		}

		// Sort keys for stable output.
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		total := 0
		for _, k := range keys {
			fmt.Printf("%-20s %d\n", k, counts[k])
			total += counts[k]
		}
		fmt.Printf("%-20s %d\n", "TOTAL", total)
		return nil
	},
}

func init() {
	countCmd.Flags().String("by", "status", "group by: status, type, priority, assignee, label")
	countCmd.Flags().StringP("status", "s", "", "filter by status before counting")
	rootCmd.AddCommand(countCmd)
}
