package store

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RamXX/nd/internal/model"
)

// FilterOptions controls which issues ListIssues returns.
type FilterOptions struct {
	Status        string
	Type          string
	Assignee      string
	Label         string
	Priority      string // filter by priority (e.g. "1", "P1")
	Parent        string // filter by parent ID
	NoParent      bool   // issues with no parent
	CreatedAfter  time.Time
	CreatedBefore time.Time
	UpdatedAfter  time.Time
	UpdatedBefore time.Time
	Sort          string // "priority", "created", "updated", "id" (default)
	Reverse       bool
	Limit         int
}

// ListIssues reads all issues from the vault and applies filters.
func (s *Store) ListIssues(opts FilterOptions) ([]*model.Issue, error) {
	files, err := s.vault.Files("issues", "md")
	if err != nil {
		return nil, err
	}

	type result struct {
		issue *model.Issue
		err   error
	}

	results := make([]result, len(files))
	var wg sync.WaitGroup

	for i, f := range files {
		wg.Add(1)
		go func(idx int, file string) {
			defer wg.Done()
			// Extract ID from filename (strip issues/ prefix and .md suffix).
			base := filepath.Base(file)
			id := strings.TrimSuffix(base, ".md")
			issue, err := s.ReadIssue(id)
			results[idx] = result{issue: issue, err: err}
		}(i, f)
	}
	wg.Wait()

	var issues []*model.Issue
	for _, r := range results {
		if r.err != nil {
			continue // skip unreadable issues
		}
		if s.MatchesFilter(r.issue, opts) {
			issues = issues[:len(issues):len(issues)]
			issues = append(issues, r.issue)
		}
	}

	SortIssues(issues, opts.Sort, opts.Reverse)

	if opts.Limit > 0 && len(issues) > opts.Limit {
		issues = issues[:opts.Limit]
	}
	return issues, nil
}

// MatchesFilter reports whether an issue matches the given filter options.
func (s *Store) MatchesFilter(issue *model.Issue, opts FilterOptions) bool {
	if opts.Status != "" {
		switch opts.Status {
		case "all":
			// No status filter.
		case "!closed":
			if issue.Status == model.StatusClosed {
				return false
			}
		default:
			st, err := model.ParseStatusWithCustom(opts.Status, s.CustomStatuses())
			if err != nil {
				return false
			}
			if issue.Status != st {
				return false
			}
		}
	}
	if opts.Type != "" {
		t, err := model.ParseIssueType(opts.Type)
		if err != nil {
			return false
		}
		if issue.Type != t {
			return false
		}
	}
	if opts.Assignee != "" && !strings.EqualFold(issue.Assignee, opts.Assignee) {
		return false
	}
	if opts.Label != "" {
		found := false
		for _, l := range issue.Labels {
			if strings.EqualFold(l, opts.Label) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if opts.Priority != "" {
		p, err := model.ParsePriority(opts.Priority)
		if err != nil {
			return false
		}
		if issue.Priority != p {
			return false
		}
	}
	if opts.Parent != "" && issue.Parent != opts.Parent {
		return false
	}
	if opts.NoParent && issue.Parent != "" {
		return false
	}
	if !opts.CreatedAfter.IsZero() && !issue.CreatedAt.After(opts.CreatedAfter) {
		return false
	}
	if !opts.CreatedBefore.IsZero() && !issue.CreatedAt.Before(opts.CreatedBefore) {
		return false
	}
	if !opts.UpdatedAfter.IsZero() && !issue.UpdatedAt.After(opts.UpdatedAfter) {
		return false
	}
	if !opts.UpdatedBefore.IsZero() && !issue.UpdatedAt.Before(opts.UpdatedBefore) {
		return false
	}
	return true
}

// SortIssues sorts issues by the given field. Supported values: priority,
// created, updated, id (default). If reverse is true, the order is inverted.
func SortIssues(issues []*model.Issue, sortBy string, reverse bool) {
	less := func(a, b *model.Issue) bool { return a.ID < b.ID }
	switch sortBy {
	case "priority":
		less = func(a, b *model.Issue) bool { return a.Priority < b.Priority }
	case "created":
		less = func(a, b *model.Issue) bool { return a.CreatedAt.Before(b.CreatedAt) }
	case "updated":
		less = func(a, b *model.Issue) bool { return a.UpdatedAt.After(b.UpdatedAt) }
	}
	if reverse {
		orig := less
		less = func(a, b *model.Issue) bool { return orig(b, a) }
	}
	sortByFunc(issues, less)
}

func sortByFunc(issues []*model.Issue, less func(a, b *model.Issue) bool) {
	// Simple insertion sort -- issue counts are small.
	for i := 1; i < len(issues); i++ {
		for j := i; j > 0 && less(issues[j], issues[j-1]); j-- {
			issues[j], issues[j-1] = issues[j-1], issues[j]
		}
	}
}
