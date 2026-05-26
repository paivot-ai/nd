package cmd

import (
	"fmt"
	"os"
	"os/user"

	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new nd vault",
	RunE: func(cmd *cobra.Command, args []string) error {
		prefix, _ := cmd.Flags().GetString("prefix")
		author, _ := cmd.Flags().GetString("author")
		trackIssues, _ := cmd.Flags().GetBool("track-issues")
		dir := resolveVaultDir()

		if prefix == "" {
			var source string
			prefix, source = inferPrefix()
			if prefix == "" {
				return fmt.Errorf("--prefix is required (could not infer from git remote or directory name)")
			}
			if !quiet {
				fmt.Printf("Inferred prefix: %s (from %s)\n", prefix, source)
			}
		}
		if author == "" {
			u, err := user.Current()
			if err == nil {
				author = u.Username
			} else {
				author = "unknown"
			}
		}

		// Check if already initialized.
		if _, err := os.Stat(dir + "/.nd.yaml"); err == nil {
			return fmt.Errorf("vault already initialized at %s", dir)
		}

		s, err := store.Init(dir, prefix, author, store.InitOptions{TrackIssues: trackIssues})
		if err != nil {
			return err
		}
		if !quiet {
			fmt.Printf("Initialized nd vault at %s (prefix: %s)\n", s.Dir(), prefix)
		}
		return nil
	},
}

func init() {
	initCmd.Flags().String("prefix", "", "issue ID prefix (required)")
	initCmd.Flags().String("author", "", "default author (defaults to OS user)")
	initCmd.Flags().Bool("track-issues", false, "keep .nd.yaml and issues/ git-tracked instead of ignored")
	rootCmd.AddCommand(initCmd)
}
