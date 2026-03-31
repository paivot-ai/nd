package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update issue fields",
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

		changed := false

		if cmd.Flags().Changed("status") {
			v, _ := cmd.Flags().GetString("status")
			st, err := model.ParseStatusWithCustom(v, s.CustomStatuses())
			if err != nil {
				return err
			}
			if err := s.UpdateStatus(id, st); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("title") {
			v, _ := cmd.Flags().GetString("title")
			if err := s.UpdateField(id, "title", fmt.Sprintf("%q", v)); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("priority") {
			v, _ := cmd.Flags().GetString("priority")
			p, err := model.ParsePriority(v)
			if err != nil {
				return err
			}
			if err := s.UpdateField(id, "priority", fmt.Sprintf("%d", p)); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("assignee") {
			v, _ := cmd.Flags().GetString("assignee")
			if err := s.UpdateField(id, "assignee", v); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("type") {
			v, _ := cmd.Flags().GetString("type")
			if _, err := model.ParseIssueType(v); err != nil {
				return err
			}
			if err := s.UpdateField(id, "type", v); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("append-notes") {
			v, _ := cmd.Flags().GetString("append-notes")
			if err := s.AppendNotes(id, v); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("description") {
			v, _ := cmd.Flags().GetString("description")
			if err := s.UpdateDescription(id, v); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("body-file") {
			bf, _ := cmd.Flags().GetString("body-file")
			body, err := readBodyFile(bf)
			if err != nil {
				return err
			}
			if err := s.UpdateDescription(id, body); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("parent") {
			v, _ := cmd.Flags().GetString("parent")
			if err := s.SetParent(id, v); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("follows") {
			v, _ := cmd.Flags().GetString("follows")
			if err := s.AddFollows(id, v); err != nil {
				return err
			}
			changed = true
		}
		if cmd.Flags().Changed("unfollow") {
			v, _ := cmd.Flags().GetString("unfollow")
			if err := s.RemoveFollows(id, v); err != nil {
				return err
			}
			changed = true
		}

		if cmd.Flags().Changed("set-labels") {
			v, _ := cmd.Flags().GetString("set-labels")
			if v == "" {
				if err := s.Vault().PropertyRemove(id, "labels"); err != nil {
					return err
				}
			} else {
				var labels []string
				for _, l := range strings.Split(v, ",") {
					l = strings.TrimSpace(l)
					if l != "" {
						labels = append(labels, l)
					}
				}
				value := fmt.Sprintf("[%s]", strings.Join(labels, ", "))
				if err := s.UpdateField(id, "labels", value); err != nil {
					return err
				}
			}
			changed = true
		}

		if cmd.Flags().Changed("add-label") || cmd.Flags().Changed("remove-label") {
			issue, err := s.ReadIssue(id)
			if err != nil {
				return err
			}
			labels := issue.Labels

			if cmd.Flags().Changed("add-label") {
				toAdd, _ := cmd.Flags().GetStringSlice("add-label")
				for _, add := range toAdd {
					found := false
					for _, l := range labels {
						if strings.EqualFold(l, add) {
							found = true
							break
						}
					}
					if !found {
						labels = append(labels, add)
					}
				}
			}

			if cmd.Flags().Changed("remove-label") {
				toRemove, _ := cmd.Flags().GetStringSlice("remove-label")
				var filtered []string
				for _, l := range labels {
					keep := true
					for _, rm := range toRemove {
						if strings.EqualFold(l, rm) {
							keep = false
							break
						}
					}
					if keep {
						filtered = append(filtered, l)
					}
				}
				labels = filtered
			}

			if len(labels) == 0 {
				if err := s.Vault().PropertyRemove(id, "labels"); err != nil {
					return err
				}
			} else {
				value := fmt.Sprintf("[%s]", strings.Join(labels, ", "))
				if err := s.UpdateField(id, "labels", value); err != nil {
					return err
				}
			}
			changed = true
		}

		if cmd.Flags().Changed("comment") {
			v, _ := cmd.Flags().GetString("comment")
			now := time.Now().UTC().Format(time.RFC3339)
			author := s.Config().CreatedBy
			comment := fmt.Sprintf("\n### %s %s\n%s\n", now, author, v)
			if err := s.Vault().Append(id, comment, false); err != nil {
				return fmt.Errorf("append comment: %w", err)
			}
			changed = true
		}

		if !changed {
			return fmt.Errorf("no fields specified to update")
		}

		if !quiet {
			fmt.Printf("Updated %s\n", id)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().String("status", "", "new status")
	updateCmd.Flags().String("title", "", "new title")
	updateCmd.Flags().String("priority", "", "new priority (0-4 or P0-P4)")
	updateCmd.Flags().String("assignee", "", "new assignee")
	updateCmd.Flags().String("type", "", "new type")
	updateCmd.Flags().String("append-notes", "", "append text to Notes section")
	updateCmd.Flags().StringP("description", "d", "", "new Description section content")
	updateCmd.Flags().String("parent", "", "set parent issue ID (empty to clear)")
	updateCmd.Flags().String("body-file", "", "read Description section content from file (- for stdin)")
	updateCmd.Flags().String("follows", "", "add follows link to predecessor issue")
	updateCmd.Flags().String("unfollow", "", "remove follows link from predecessor issue")
	updateCmd.Flags().String("set-labels", "", "replace all labels (comma-separated, empty to clear)")
	updateCmd.Flags().StringSlice("add-label", nil, "add labels")
	updateCmd.Flags().StringSlice("remove-label", nil, "remove labels")
	updateCmd.Flags().String("comment", "", "add a comment (shorthand for comments add)")
	rootCmd.AddCommand(updateCmd)
}
