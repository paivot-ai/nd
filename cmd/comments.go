package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

var commentsCmd = &cobra.Command{
	Use:   "comments",
	Short: "Manage comments on issues",
}

var commentsAddCmd = &cobra.Command{
	Use:   "add <id> <text>",
	Short: "Add a comment to an issue",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		text := strings.Join(args[1:], " ")

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		// Verify issue exists.
		if _, err := s.ReadIssue(id); err != nil {
			return fmt.Errorf("issue %s not found: %w", id, err)
		}

		now := time.Now().UTC().Format(time.RFC3339)
		author := s.Config().CreatedBy
		comment := fmt.Sprintf("\n### %s %s\n%s\n", now, author, text)

		if err := s.Vault().Append(id, comment, false); err != nil {
			return fmt.Errorf("append comment: %w", err)
		}

		if !quiet {
			fmt.Printf("Comment added to %s\n", id)
		}
		return nil
	},
}

var commentsListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List comments on an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		// Pass the heading WITH the markdown prefix. vlt v0.8.x's findSection
		// requires it (the bare-text form was added in newer vlt). Without
		// the "## " prefix every comments-list call returns "heading not found"
		// even when the section exists.
		content, err := s.Vault().Read(id, "## Comments")
		if err != nil {
			return err
		}

		if strings.TrimSpace(content) == "" {
			fmt.Println("No comments.")
			return nil
		}
		fmt.Print(content)
		return nil
	},
}

func init() {
	commentsCmd.AddCommand(commentsAddCmd)
	commentsCmd.AddCommand(commentsListCmd)
	rootCmd.AddCommand(commentsCmd)
}
