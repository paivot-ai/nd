package enforce

import (
	"fmt"

	"github.com/paivot-ai/nd/internal/model"
)

// ValidateIssue runs structural validation on an issue.
func ValidateIssue(issue *model.Issue) error {
	return issue.Validate()
}

// ValidateIssueWithCustom validates an issue accepting custom statuses.
func ValidateIssueWithCustom(issue *model.Issue, custom []model.Status) error {
	return issue.ValidateWithCustom(custom)
}

// ValidateDeps checks that dependency references don't form obvious problems:
// - An issue cannot block itself.
// - blocks and blocked_by should not overlap for the same ID.
func ValidateDeps(issue *model.Issue) error {
	for _, b := range issue.Blocks {
		if b == issue.ID {
			return fmt.Errorf("issue %s cannot block itself", issue.ID)
		}
	}
	for _, b := range issue.BlockedBy {
		if b == issue.ID {
			return fmt.Errorf("issue %s cannot be blocked by itself", issue.ID)
		}
	}
	return nil
}
