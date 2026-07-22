package store

import (
	"fmt"
	"strings"

	"github.com/paivot-ai/nd/internal/model"
)

// ClaimIssue atomically claims an issue for an agent: the read, ownership
// check, and write all happen under the vault's exclusive lock, so two agents
// racing for the same story cannot both win. Claiming sets the assignee and
// moves the issue to in_progress (FSM-validated).
//
// Rules:
//   - closed issues cannot be claimed
//   - an issue already claimed by another agent is refused unless force is set
//   - re-claiming your own issue is an idempotent no-op
//   - an issue with open blockers is refused unless force is set
func (s *Store) ClaimIssue(id, agent string, force bool) (*model.Issue, error) {
	if strings.TrimSpace(agent) == "" {
		return nil, fmt.Errorf("claim requires an agent name")
	}

	issue, err := s.ReadIssue(id)
	if err != nil {
		return nil, err
	}

	if issue.Status == model.StatusClosed {
		return nil, fmt.Errorf("cannot claim closed issue %s", id)
	}

	if issue.Assignee != "" && !strings.EqualFold(issue.Assignee, agent) && !force {
		return nil, fmt.Errorf("issue %s is already claimed by %s (use --force to steal)", id, issue.Assignee)
	}

	if issue.Status == model.StatusInProgress && strings.EqualFold(issue.Assignee, agent) {
		return issue, nil // idempotent re-claim
	}

	if !force {
		for _, blockerID := range issue.BlockedBy {
			blocker, berr := s.ReadIssue(blockerID)
			if berr != nil {
				continue // orphan reference; doctor's problem, not claim's
			}
			if blocker.Status != model.StatusClosed {
				return nil, fmt.Errorf("issue %s is blocked by open issue %s (use --force to claim anyway)", id, blockerID)
			}
		}
	}

	stolen := issue.Assignee != "" && !strings.EqualFold(issue.Assignee, agent)

	if err := s.vault.PropertySet(id, "assignee", agent); err != nil {
		return nil, err
	}
	if issue.Status != model.StatusInProgress {
		if err := s.UpdateStatus(id, model.StatusInProgress); err != nil {
			// Roll the assignee back so a failed FSM transition does not
			// leave a half-claimed issue.
			if issue.Assignee == "" {
				_ = s.vault.PropertyRemove(id, "assignee")
			} else {
				_ = s.vault.PropertySet(id, "assignee", issue.Assignee)
			}
			return nil, err
		}
	} else if err := s.touchUpdatedAt(id); err != nil {
		return nil, err
	}

	entry := fmt.Sprintf("claimed by %s", agent)
	if stolen {
		entry = fmt.Sprintf("claimed by %s (stolen from %s)", agent, issue.Assignee)
	}
	_ = s.appendHistory(id, entry)

	return s.ReadIssue(id)
}

// ReleaseIssue releases a claim: clears the assignee and returns the issue to
// open. Only the claiming agent may release without force.
func (s *Store) ReleaseIssue(id, agent string, force bool) (*model.Issue, error) {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return nil, err
	}
	if issue.Status == model.StatusClosed {
		return nil, fmt.Errorf("issue %s is closed; use reopen instead of release", id)
	}
	if issue.Assignee == "" && issue.Status != model.StatusInProgress {
		return issue, nil // nothing to release
	}
	if issue.Assignee != "" && !strings.EqualFold(issue.Assignee, agent) && !force {
		return nil, fmt.Errorf("issue %s is claimed by %s, not %s (use --force)", id, issue.Assignee, agent)
	}

	if err := s.vault.PropertyRemove(id, "assignee"); err != nil {
		return nil, err
	}
	if issue.Status == model.StatusInProgress {
		if err := s.UpdateStatus(id, model.StatusOpen); err != nil {
			return nil, err
		}
	} else if err := s.touchUpdatedAt(id); err != nil {
		return nil, err
	}
	_ = s.appendHistory(id, fmt.Sprintf("released by %s", agent))

	return s.ReadIssue(id)
}
