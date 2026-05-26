package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/paivot-ai/nd/internal/graph"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var epicCmd = &cobra.Command{
	Use:   "epic",
	Short: "Epic management commands",
}

var epicStatusCmd = &cobra.Command{
	Use:   "status <id>",
	Short: "Show epic progress summary",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
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
		summary := g.EpicStatus(id)
		if summary == nil {
			return fmt.Errorf("epic %s not found", id)
		}

		fmt.Printf("Epic: %s - %s\n", summary.Epic.ID, summary.Epic.Title)
		fmt.Printf("Children: %d total\n", summary.Total)
		fmt.Printf("  Open:        %d\n", summary.Open)
		fmt.Printf("  In Progress: %d\n", summary.InProgress)
		fmt.Printf("  Blocked:     %d\n", summary.Blocked)
		fmt.Printf("  Closed:      %d\n", summary.Closed)
		if summary.Total > 0 {
			pct := float64(summary.Closed) / float64(summary.Total) * 100
			fmt.Printf("  Progress:    %.0f%%\n", pct)
		}
		return nil
	},
}

var epicTreeCmd = &cobra.Command{
	Use:   "tree <id>",
	Short: "Show epic as a tree",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
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
		tree := g.EpicTree(id)
		if tree == nil {
			return fmt.Errorf("epic %s not found", id)
		}

		printTree(os.Stdout, tree, 0)
		return nil
	},
}

func printTree(w *os.File, node *graph.EpicNode, depth int) {
	prefix := strings.Repeat("  ", depth)
	marker := ""
	switch node.Issue.Status {
	case "closed":
		marker = "[x]"
	case "in_progress":
		marker = "[>]"
	case "blocked":
		marker = "[!]"
	default:
		marker = "[ ]"
	}
	fmt.Fprintf(w, "%s%s %s %s (%s)\n", prefix, marker, node.Issue.ID, node.Issue.Title, node.Issue.Priority.Short())
	for _, child := range node.Children {
		printTree(w, child, depth+1)
	}
}

var epicCloseEligibleCmd = &cobra.Command{
	Use:   "close-eligible",
	Short: "List epics where all children are closed",
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
		epics := g.Epics()

		found := false
		for _, epic := range epics {
			if epic.Status == "closed" {
				continue
			}
			summary := g.EpicStatus(epic.ID)
			if summary != nil && summary.Total > 0 && summary.Closed == summary.Total {
				fmt.Printf("%s %s (%d/%d closed)\n", epic.ID, epic.Title, summary.Closed, summary.Total)
				found = true
			}
		}
		if !found {
			fmt.Println("No close-eligible epics found.")
		}
		return nil
	},
}

func init() {
	epicCmd.AddCommand(epicStatusCmd)
	epicCmd.AddCommand(epicTreeCmd)
	epicCmd.AddCommand(epicCloseEligibleCmd)
	rootCmd.AddCommand(epicCmd)
}
