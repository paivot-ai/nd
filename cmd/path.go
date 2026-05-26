package cmd

import (
	"fmt"
	"os"

	"github.com/paivot-ai/nd/internal/graph"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var pathCmd = &cobra.Command{
	Use:   "path [id]",
	Short: "Show execution path as terminal tree",
	Long:  "Without id: shows all path roots (start of chains). With id: shows the execution chain from that issue.",
	Args:  cobra.MaximumNArgs(1),
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

		if len(args) == 1 {
			id := args[0]
			tree := g.ExecutionPath(id)
			if tree == nil {
				return fmt.Errorf("issue %s not found", id)
			}
			printPathNode(tree, "", true)
			return nil
		}

		roots := g.PathRoots()
		if len(roots) == 0 {
			fmt.Println("No execution paths to display.")
			return nil
		}

		for i, root := range roots {
			tree := g.ExecutionPath(root.ID)
			if tree == nil {
				continue
			}
			printPathNode(tree, "", true)
			if i < len(roots)-1 {
				fmt.Println()
			}
		}
		return nil
	},
}

func printPathNode(node *graph.PathNode, prefix string, isLast bool) {
	connector := "|- "
	if isLast {
		connector = "`- "
	}
	if prefix == "" {
		connector = ""
	}

	icon := statusIcon(node.Issue.Status)
	fmt.Fprintf(os.Stdout, "%s%s%s %s %s (%s)\n",
		prefix, connector, icon, node.Issue.ID, node.Issue.Title, node.Issue.Priority.Short())

	childPrefix := prefix
	if prefix != "" {
		if isLast {
			childPrefix += "   "
		} else {
			childPrefix += "|  "
		}
	}

	for i, child := range node.Children {
		printPathNode(child, childPrefix, i == len(node.Children)-1)
	}
}

func init() {
	rootCmd.AddCommand(pathCmd)
}
