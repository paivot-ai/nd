package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/paivot-ai/nd/internal/graph"
	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var graphCmd = &cobra.Command{
	Use:   "graph [id]",
	Short: "Show dependency graph as terminal DAG",
	Long:  "Without id: shows all root issues (no blockers). With id: shows the subgraph reachable from that issue.",
	Args:  cobra.MaximumNArgs(1),
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

		if len(args) == 1 {
			// Show subgraph from a specific issue.
			id := args[0]
			tree := g.DepTree(id)
			if tree == nil {
				return fmt.Errorf("issue %s not found", id)
			}
			printGraphNode(tree, "", true)
			return nil
		}

		// Show all roots (issues that don't block anything and aren't blocked).
		// Actually: show all "root" nodes in the dep graph (no blockers).
		roots := findRoots(all)
		if len(roots) == 0 {
			fmt.Println("No dependency graph to display.")
			return nil
		}

		for i, root := range roots {
			tree := g.DepTree(root.ID)
			if tree == nil {
				continue
			}
			printGraphNode(tree, "", true)
			if i < len(roots)-1 {
				fmt.Println()
			}
		}
		return nil
	},
}

// findRoots returns issues that have blocks but no blocked_by (DAG entry points).
// If no such issues exist, returns all issues that have any dep relationships.
func findRoots(issues []*model.Issue) []*model.Issue {
	var roots []*model.Issue
	for _, issue := range issues {
		if len(issue.Blocks) > 0 && len(issue.BlockedBy) == 0 {
			roots = append(roots, issue)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].ID < roots[j].ID
	})
	return roots
}

func printGraphNode(node *graph.DepNode, prefix string, isLast bool) {
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
		printGraphNode(child, childPrefix, i == len(node.Children)-1)
	}
}

func statusIcon(s model.Status) string {
	switch s {
	case model.StatusClosed:
		return "[x]"
	case model.StatusInProgress:
		return "[>]"
	case model.StatusBlocked:
		return "[!]"
	case model.StatusDeferred:
		return "[-]"
	default:
		return "[ ]"
	}
}

func init() {
	rootCmd.AddCommand(graphCmd)
}
