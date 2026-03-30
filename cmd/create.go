package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new issue",
	Args:  cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		titleFlag, _ := cmd.Flags().GetString("title")
		title := strings.Join(args, " ")
		if titleFlag != "" {
			if title != "" {
				return fmt.Errorf("provide title as positional argument or --title, not both")
			}
			title = titleFlag
		}
		if title == "" {
			return fmt.Errorf("title is required (positional argument or --title)")
		}
		issueType, _ := cmd.Flags().GetString("type")
		priorityStr, _ := cmd.Flags().GetString("priority")
		prio, err := model.ParsePriority(priorityStr)
		if err != nil {
			return err
		}
		priority := int(prio)
		assignee, _ := cmd.Flags().GetString("assignee")
		labelsStr, _ := cmd.Flags().GetString("labels")
		description, _ := cmd.Flags().GetString("description")
		parent, _ := cmd.Flags().GetString("parent")
		bodyFile, _ := cmd.Flags().GetString("body-file")

		if bodyFile != "" {
			body, err := readBodyFile(bodyFile)
			if err != nil {
				return err
			}
			description = body
		}

		if issueType == "" {
			issueType = "task"
		}

		var labels []string
		if labelsStr != "" {
			for _, l := range strings.Split(labelsStr, ",") {
				l = strings.TrimSpace(l)
				if l != "" {
					labels = append(labels, l)
				}
			}
		}

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issue, err := s.CreateIssue(title, description, issueType, priority, assignee, labels, parent)
		if err != nil {
			return err
		}

		if jsonOut {
			fmt.Printf(`{"id":"%s"}`, issue.ID)
			fmt.Println()
		} else if !quiet {
			fmt.Printf("Created %s: %s\n", issue.ID, issue.Title)
		} else {
			fmt.Println(issue.ID)
		}
		return nil
	},
}

func init() {
	createCmd.Flags().String("title", "", "issue title (alternative to positional argument)")
	createCmd.Flags().StringP("type", "t", "task", "issue type (bug, feature, task, epic, chore, decision)")
	createCmd.Flags().StringP("priority", "p", "2", "priority (0-4 or P0-P4, default P2)")
	createCmd.Flags().String("assignee", "", "assignee")
	createCmd.Flags().String("labels", "", "comma-separated labels")
	createCmd.Flags().StringP("description", "d", "", "issue description")
	createCmd.Flags().String("parent", "", "parent issue ID")
	createCmd.Flags().String("body-file", "", "read description from file (- for stdin)")
	rootCmd.AddCommand(createCmd)
}

// readBodyFile reads content from a file path or stdin (when path is "-").
func readBodyFile(path string) (string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read body file: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}
