package cmd

import (
	"fmt"
	"strings"

	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

var qCmd = &cobra.Command{
	Use:   "q [title]",
	Short: "Quick capture -- create issue, print only ID",
	Long:  "Create an issue and print only its ID to stdout, enabling ISSUE=$(nd q \"title\").",
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
		} else {
			fmt.Println(issue.ID)
		}
		return nil
	},
}

func init() {
	qCmd.Flags().String("title", "", "issue title (alternative to positional argument)")
	qCmd.Flags().StringP("type", "t", "task", "issue type")
	qCmd.Flags().StringP("priority", "p", "2", "priority (0-4 or P0-P4, default P2)")
	qCmd.Flags().String("assignee", "", "assignee")
	qCmd.Flags().String("labels", "", "comma-separated labels")
	qCmd.Flags().StringP("description", "d", "", "issue description")
	qCmd.Flags().String("parent", "", "parent issue ID")
	qCmd.Flags().String("body-file", "", "read description from file (- for stdin)")
	rootCmd.AddCommand(qCmd)
}
