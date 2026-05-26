package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Open issue in $EDITOR",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		// Verify issue exists.
		if _, err := s.ReadIssue(id); err != nil {
			return fmt.Errorf("issue %s not found: %w", id, err)
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			editor = "vi"
		}

		path := filepath.Join(s.Dir(), "issues", id+".md")
		c := exec.Command(editor, path)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("editor: %w", err)
		}

		// Refresh hash and links after edit.
		if err := s.RefreshAfterEdit(id); err != nil {
			return fmt.Errorf("refresh after edit: %w", err)
		}

		if !quiet {
			fmt.Printf("Updated %s\n", id)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(editCmd)
}
