package cmd

import (
	"fmt"
	"strings"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var labelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "Manage labels on issues",
}

var labelsAddCmd = &cobra.Command{
	Use:   "add <id> <label>",
	Short: "Add a label to an issue",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, label := args[0], args[1]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		issue, err := s.ReadIssue(id)
		if err != nil {
			return err
		}

		for _, l := range issue.Labels {
			if strings.EqualFold(l, label) {
				return fmt.Errorf("label %q already exists on %s", label, id)
			}
		}

		newLabels := append(issue.Labels, label)
		value := fmt.Sprintf("[%s]", strings.Join(newLabels, ", "))
		if err := s.UpdateField(id, "labels", value); err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("Added label %q to %s\n", label, id)
		}
		return nil
	},
}

var labelsRmCmd = &cobra.Command{
	Use:   "rm <id> <label>",
	Short: "Remove a label from an issue",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, label := args[0], args[1]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		issue, err := s.ReadIssue(id)
		if err != nil {
			return err
		}

		var newLabels []string
		found := false
		for _, l := range issue.Labels {
			if strings.EqualFold(l, label) {
				found = true
				continue
			}
			newLabels = append(newLabels, l)
		}
		if !found {
			return fmt.Errorf("label %q not found on %s", label, id)
		}

		if len(newLabels) == 0 {
			if err := s.Vault().PropertyRemove(id, "labels"); err != nil {
				return err
			}
		} else {
			value := fmt.Sprintf("[%s]", strings.Join(newLabels, ", "))
			if err := s.UpdateField(id, "labels", value); err != nil {
				return err
			}
		}
		if !quiet {
			fmt.Printf("Removed label %q from %s\n", label, id)
		}
		return nil
	},
}

var labelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all labels across issues",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		issues, err := s.ListIssues(store.FilterOptions{})
		if err != nil {
			return err
		}

		counts := make(map[string]int)
		for _, issue := range issues {
			for _, l := range issue.Labels {
				counts[l]++
			}
		}

		if len(counts) == 0 {
			fmt.Println("No labels found.")
			return nil
		}
		for label, count := range counts {
			fmt.Printf("%-20s %d\n", label, count)
		}
		return nil
	},
}

func init() {
	labelsCmd.AddCommand(labelsAddCmd)
	labelsCmd.AddCommand(labelsRmCmd)
	labelsCmd.AddCommand(labelsListCmd)
	rootCmd.AddCommand(labelsCmd)
}
