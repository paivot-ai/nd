package cmd

import (
	"fmt"

	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

// Aliases for commonly hallucinated commands.
// Hidden so they don't clutter --help, but work when called.

var resolveCmd = &cobra.Command{
	Use:    "resolve <issue> <dependency>",
	Short:  "Remove a dependency (alias for dep rm)",
	Long:   "Alias for `nd dep rm`. Removes the dependency: issue no longer depends on dependency.",
	Args:   cobra.ExactArgs(2),
	Hidden: true,
	RunE:   depRmRunE,
}

var unblockCmd = &cobra.Command{
	Use:    "unblock <issue> <dependency>",
	Short:  "Remove a dependency (alias for dep rm)",
	Long:   "Alias for `nd dep rm`. Removes the dependency: issue no longer depends on dependency.",
	Args:   cobra.ExactArgs(2),
	Hidden: true,
	RunE:   depRmRunE,
}

var blockCmd = &cobra.Command{
	Use:    "block <issue> <dependency>",
	Short:  "Add a dependency (alias for dep add)",
	Long:   "Alias for `nd dep add`. Adds the dependency: issue depends on dependency.",
	Args:   cobra.ExactArgs(2),
	Hidden: true,
	RunE:   depAddRunE,
}

var startCmd = &cobra.Command{
	Use:    "start <issue>",
	Short:  "Start working on an issue (alias for update --status=in_progress)",
	Long:   "Alias for `nd update <issue> --status=in_progress`. Transitions the issue to in_progress.",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.UpdateStatus(id, model.StatusInProgress); err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("Started %s\n", id)
		}
		return nil
	},
}

// depRmRunE is the shared RunE for resolve and unblock aliases.
func depRmRunE(cmd *cobra.Command, args []string) error {
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
}

// depAddRunE is the shared RunE for the block alias.
func depAddRunE(cmd *cobra.Command, args []string) error {
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
}

// commentCmd is a hidden alias for "comments" (singular form).
// Agents consistently try "nd comment add" instead of "nd comments add".
var commentCmd = &cobra.Command{
	Use:    "comment",
	Short:  "Manage comments (alias for comments)",
	Hidden: true,
}

func init() {
	// Copy subcommands from commentsCmd to the singular alias.
	commentCmd.AddCommand(&cobra.Command{
		Use:   "add <id> <text>",
		Short: "Add a comment (alias for comments add)",
		Args:  cobra.MinimumNArgs(2),
		RunE:  commentsAddCmd.RunE,
	})
	commentCmd.AddCommand(&cobra.Command{
		Use:   "list <id>",
		Short: "List comments (alias for comments list)",
		Args:  cobra.ExactArgs(1),
		RunE:  commentsListCmd.RunE,
	})

	rootCmd.AddCommand(resolveCmd)
	rootCmd.AddCommand(unblockCmd)
	rootCmd.AddCommand(blockCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(commentCmd)
}
