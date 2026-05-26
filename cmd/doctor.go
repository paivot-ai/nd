package cmd

import (
	"fmt"
	"strings"

	"github.com/paivot-ai/nd/internal/enforce"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Validate vault integrity",
	RunE: func(cmd *cobra.Command, args []string) error {
		fix, _ := cmd.Flags().GetBool("fix")

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issues, err := s.ListIssues(store.FilterOptions{})
		if err != nil {
			return err
		}

		problems := 0

		// Check 1: Content hash verification.
		for _, issue := range issues {
			expected := enforce.ComputeContentHash(issue.Body)
			if issue.ContentHash != expected {
				fmt.Printf("[HASH] %s: content hash mismatch (stored: %s, computed: %s)\n",
					issue.ID, issue.ContentHash[:20]+"...", expected[:20]+"...")
				problems++
				if fix {
					if err := s.UpdateField(issue.ID, "content_hash", fmt.Sprintf("%q", expected)); err != nil {
						errorf("fix hash %s: %v", issue.ID, err)
					} else {
						fmt.Printf("  -> fixed\n")
					}
				}
			}
		}

		// Check 2: Bidirectional dependency consistency.
		issueMap := make(map[string]bool)
		for _, issue := range issues {
			issueMap[issue.ID] = true
		}

		for _, issue := range issues {
			// Check that blocks references exist.
			for _, b := range issue.Blocks {
				if !issueMap[b] {
					fmt.Printf("[DEP] %s blocks %s, but %s does not exist\n", issue.ID, b, b)
					problems++
					if fix {
						_ = s.RemoveDependency(b, issue.ID)
						fmt.Printf("  -> removed orphan block reference\n")
					}
				}
			}
			// Check that blocked_by references exist.
			for _, b := range issue.BlockedBy {
				if !issueMap[b] {
					fmt.Printf("[DEP] %s blocked by %s, but %s does not exist\n", issue.ID, b, b)
					problems++
					if fix {
						_ = s.RemoveDependency(issue.ID, b)
						fmt.Printf("  -> removed orphan blocked_by reference\n")
					}
				}
			}
		}

		// Check 3: Bidirectional consistency (if A blocks B, B should list A in blocked_by).
		blockedByMap := make(map[string]map[string]bool)
		blocksMap := make(map[string]map[string]bool)
		for _, issue := range issues {
			blockedByMap[issue.ID] = make(map[string]bool)
			blocksMap[issue.ID] = make(map[string]bool)
			for _, b := range issue.BlockedBy {
				blockedByMap[issue.ID][b] = true
			}
			for _, b := range issue.Blocks {
				blocksMap[issue.ID][b] = true
			}
		}

		for _, issue := range issues {
			for _, b := range issue.Blocks {
				if !blockedByMap[b][issue.ID] {
					fmt.Printf("[SYNC] %s blocks %s, but %s doesn't list %s in blocked_by\n",
						issue.ID, b, b, issue.ID)
					problems++
					if fix {
						_ = s.AddDependency(b, issue.ID)
						fmt.Printf("  -> fixed bidirectional reference\n")
					}
				}
			}
			for _, b := range issue.BlockedBy {
				if !blocksMap[b][issue.ID] {
					fmt.Printf("[SYNC] %s blocked by %s, but %s doesn't list %s in blocks\n",
						issue.ID, b, b, issue.ID)
					problems++
					if fix {
						_ = s.AddDependency(issue.ID, b)
						fmt.Printf("  -> fixed bidirectional reference\n")
					}
				}
			}
		}

		// Check 4: Validation.
		for _, issue := range issues {
			if err := enforce.ValidateIssueWithCustom(issue, s.CustomStatuses()); err != nil {
				fmt.Printf("[VALID] %s: %v\n", issue.ID, err)
				problems++
			}
			if err := enforce.ValidateDeps(issue); err != nil {
				fmt.Printf("[VALID] %s: %v\n", issue.ID, err)
				problems++
			}
		}

		// Check 5: Links section integrity.
		for _, issue := range issues {
			if !strings.Contains(issue.Body, "\n## Links\n") {
				fmt.Printf("[LINKS] %s: missing ## Links section\n", issue.ID)
				problems++
				if fix {
					if err := s.UpdateLinksSection(issue.ID); err != nil {
						errorf("fix links %s: %v", issue.ID, err)
					} else {
						fmt.Printf("  -> fixed\n")
					}
				}
			}
		}

		if problems == 0 {
			fmt.Printf("All %d issues passed validation.\n", len(issues))
		} else {
			action := "found"
			if fix {
				action = "fixed"
			}
			fmt.Printf("\n%d problem(s) %s across %d issues.\n", problems, action, len(issues))
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().Bool("fix", false, "attempt to fix problems")
	rootCmd.AddCommand(doctorCmd)
}
