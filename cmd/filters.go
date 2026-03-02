package cmd

import (
	"fmt"
	"time"

	"github.com/RamXX/nd/internal/store"
	"github.com/spf13/cobra"
)

// addFilterFlags registers the standard filter flags on a cobra command.
// Use buildFilterOptions to read these flags back into a FilterOptions struct.
func addFilterFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("status", "s", "", "filter by status")
	cmd.Flags().String("type", "", "filter by type")
	cmd.Flags().StringP("assignee", "a", "", "filter by assignee")
	cmd.Flags().StringP("label", "l", "", "filter by label")
	cmd.Flags().StringP("priority", "p", "", "filter by priority (0-4 or P0-P4)")
	cmd.Flags().String("parent", "", "filter by parent issue ID")
	cmd.Flags().Bool("no-parent", false, "show only issues with no parent")
	cmd.Flags().String("created-after", "", "filter by created date (YYYY-MM-DD)")
	cmd.Flags().String("created-before", "", "filter by created date (YYYY-MM-DD)")
	cmd.Flags().String("updated-after", "", "filter by updated date (YYYY-MM-DD)")
	cmd.Flags().String("updated-before", "", "filter by updated date (YYYY-MM-DD)")
	cmd.Flags().String("sort", "priority", "sort by: priority, created, updated, id")
	cmd.Flags().BoolP("reverse", "r", false, "reverse sort order")
	cmd.Flags().IntP("limit", "n", 0, "max results (0 for unlimited)")
}

// buildFilterOptions reads the standard filter flags from a cobra command
// and returns a populated FilterOptions. The defaultStatus is used when
// --status has not been explicitly set by the user.
func buildFilterOptions(cmd *cobra.Command, defaultStatus string) (store.FilterOptions, error) {
	status, _ := cmd.Flags().GetString("status")
	issueType, _ := cmd.Flags().GetString("type")
	assignee, _ := cmd.Flags().GetString("assignee")
	label, _ := cmd.Flags().GetString("label")
	priority, _ := cmd.Flags().GetString("priority")
	parent, _ := cmd.Flags().GetString("parent")
	noParent, _ := cmd.Flags().GetBool("no-parent")
	createdAfterStr, _ := cmd.Flags().GetString("created-after")
	createdBeforeStr, _ := cmd.Flags().GetString("created-before")
	updatedAfterStr, _ := cmd.Flags().GetString("updated-after")
	updatedBeforeStr, _ := cmd.Flags().GetString("updated-before")
	sortBy, _ := cmd.Flags().GetString("sort")
	reverse, _ := cmd.Flags().GetBool("reverse")
	limit, _ := cmd.Flags().GetInt("limit")

	if !cmd.Flags().Changed("status") {
		status = defaultStatus
	}

	var createdAfter, createdBefore, updatedAfter, updatedBefore time.Time
	var err error
	if createdAfter, err = parseDate(createdAfterStr, false); err != nil {
		return store.FilterOptions{}, fmt.Errorf("invalid --created-after date: %w", err)
	}
	if createdBefore, err = parseDate(createdBeforeStr, true); err != nil {
		return store.FilterOptions{}, fmt.Errorf("invalid --created-before date: %w", err)
	}
	if updatedAfter, err = parseDate(updatedAfterStr, false); err != nil {
		return store.FilterOptions{}, fmt.Errorf("invalid --updated-after date: %w", err)
	}
	if updatedBefore, err = parseDate(updatedBeforeStr, true); err != nil {
		return store.FilterOptions{}, fmt.Errorf("invalid --updated-before date: %w", err)
	}

	return store.FilterOptions{
		Status:        status,
		Type:          issueType,
		Assignee:      assignee,
		Label:         label,
		Priority:      priority,
		Parent:        parent,
		NoParent:      noParent,
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
		UpdatedAfter:  updatedAfter,
		UpdatedBefore: updatedBefore,
		Sort:          sortBy,
		Reverse:       reverse,
		Limit:         limit,
	}, nil
}

// parseDate parses a YYYY-MM-DD string into a time.Time.
// If endOfDay is true, adds 24h-1ns to include the entire day.
// Returns zero time for empty strings.
func parseDate(s string, endOfDay bool) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return t, nil
}
