package cmd

import (
	"fmt"

	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Create a compressed, git-committable snapshot of the backlog",
	Long:  "Archive issues into a tar.gz or JSONL file for compliance and backup purposes.",
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		closedOnly, _ := cmd.Flags().GetBool("closed-only")
		since, _ := cmd.Flags().GetString("since")
		format, _ := cmd.Flags().GetString("format")
		removeArchived, _ := cmd.Flags().GetBool("remove-archived")

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		opts := store.ArchiveOptions{
			Output:         output,
			ClosedOnly:     closedOnly,
			Since:          since,
			Format:         format,
			RemoveArchived: removeArchived,
		}

		path, err := s.Archive(opts, version)
		if err != nil {
			return err
		}

		if !quiet {
			fmt.Printf("Archive created: %s\n", path)
		}
		return nil
	},
}

func init() {
	archiveCmd.Flags().String("output", "", "output file path (default: .vault/archive-<date>.tar.gz)")
	archiveCmd.Flags().Bool("closed-only", false, "only archive closed issues")
	archiveCmd.Flags().String("since", "", "only archive issues updated after this date (RFC3339 or YYYY-MM-DD)")
	archiveCmd.Flags().String("format", "tar.gz", "output format (tar.gz or jsonl)")
	archiveCmd.Flags().Bool("remove-archived", false, "move archived closed issues to .trash/")
	rootCmd.AddCommand(archiveCmd)
}
