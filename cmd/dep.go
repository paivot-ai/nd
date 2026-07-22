package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/paivot-ai/nd/internal/format"
	"github.com/paivot-ai/nd/internal/graph"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var depCmd = &cobra.Command{
	Use:   "dep",
	Short: "Manage dependencies between issues",
}

var depAddCmd = &cobra.Command{
	Use:   "add <issue> <depends-on>",
	Short: "Add dependency (issue depends on depends-on)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID, depID := args[0], args[1]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.AddDependency(issueID, depID); err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("%s now depends on %s\n", issueID, depID)
		}
		return nil
	},
}

var depRmCmd = &cobra.Command{
	Use:   "rm <issue> <depends-on>",
	Short: "Remove dependency",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID, depID := args[0], args[1]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.RemoveDependency(issueID, depID); err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("Removed dependency: %s no longer depends on %s\n", issueID, depID)
		}
		return nil
	},
}

var depListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List dependencies of an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		s, err := store.OpenRead(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		issue, err := s.ReadIssue(id)
		if err != nil {
			return err
		}

		if len(issue.BlockedBy) > 0 {
			fmt.Printf("%s depends on:\n", id)
			for _, depID := range issue.BlockedBy {
				dep, err := s.ReadIssue(depID)
				if err != nil {
					fmt.Printf("  %s (unreadable)\n", depID)
					continue
				}
				format.Short(os.Stdout, dep)
			}
		}
		if len(issue.Blocks) > 0 {
			fmt.Printf("%s blocks:\n", id)
			for _, bID := range issue.Blocks {
				b, err := s.ReadIssue(bID)
				if err != nil {
					fmt.Printf("  %s (unreadable)\n", bID)
					continue
				}
				format.Short(os.Stdout, b)
			}
		}
		if len(issue.Related) > 0 {
			fmt.Printf("%s related to:\n", id)
			for _, rID := range issue.Related {
				r, err := s.ReadIssue(rID)
				if err != nil {
					fmt.Printf("  %s (unreadable)\n", rID)
					continue
				}
				format.Short(os.Stdout, r)
			}
		}
		if len(issue.BlockedBy) == 0 && len(issue.Blocks) == 0 && len(issue.Related) == 0 {
			fmt.Printf("%s has no dependencies\n", id)
		}
		return nil
	},
}

var depRelateCmd = &cobra.Command{
	Use:   "relate <issue-a> <issue-b>",
	Short: "Add bidirectional related link between two issues",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		a, b := args[0], args[1]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.AddRelated(a, b); err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("%s and %s are now related\n", a, b)
		}
		return nil
	},
}

var depUnrelateCmd = &cobra.Command{
	Use:   "unrelate <issue-a> <issue-b>",
	Short: "Remove bidirectional related link between two issues",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		a, b := args[0], args[1]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.RemoveRelated(a, b); err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("%s and %s are no longer related\n", a, b)
		}
		return nil
	},
}

var depCyclesCmd = &cobra.Command{
	Use:   "cycles",
	Short: "Detect dependency cycles",
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
		cycles := g.DetectCycles()
		if len(cycles) == 0 {
			fmt.Println("No dependency cycles found.")
			return nil
		}
		for i, cycle := range cycles {
			fmt.Printf("Cycle %d: %s\n", i+1, strings.Join(cycle, " -> "))
		}
		return nil
	},
}

var depTreeCmd = &cobra.Command{
	Use:   "tree <id>",
	Short: "Show dependency tree for an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
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
		tree := g.DepTree(id)
		if tree == nil {
			return fmt.Errorf("issue %s not found", id)
		}
		printDepTree(os.Stdout, tree, "", true)
		return nil
	},
}

func printDepTree(w *os.File, node *graph.DepNode, prefix string, isLast bool) {
	connector := "|- "
	if isLast {
		connector = "`- "
	}
	if prefix == "" {
		connector = ""
	}

	marker := "[ ]"
	switch node.Issue.Status {
	case "closed":
		marker = "[x]"
	case "in_progress":
		marker = "[>]"
	case "blocked":
		marker = "[!]"
	}
	fmt.Fprintf(w, "%s%s%s %s %s\n", prefix, connector, marker, node.Issue.ID, node.Issue.Title)

	childPrefix := prefix
	if prefix != "" {
		if isLast {
			childPrefix += "   "
		} else {
			childPrefix += "|  "
		}
	}

	for i, child := range node.Children {
		printDepTree(w, child, childPrefix, i == len(node.Children)-1)
	}
}

func init() {
	depCmd.AddCommand(depAddCmd)
	depCmd.AddCommand(depRmCmd)
	depCmd.AddCommand(depListCmd)
	depCmd.AddCommand(depRelateCmd)
	depCmd.AddCommand(depUnrelateCmd)
	depCmd.AddCommand(depCyclesCmd)
	depCmd.AddCommand(depTreeCmd)
	rootCmd.AddCommand(depCmd)
}
