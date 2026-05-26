package graph

import (
	"testing"
	"time"

	"github.com/paivot-ai/nd/internal/model"
)

func makeIssue(id string, status model.Status, blocks, blockedBy []string) *model.Issue {
	return &model.Issue{
		ID:        id,
		Title:     "Issue " + id,
		Status:    status,
		Priority:  2,
		Type:      model.TypeTask,
		CreatedAt: time.Now(),
		CreatedBy: "test",
		Blocks:    blocks,
		BlockedBy: blockedBy,
	}
}

func TestReady(t *testing.T) {
	issues := []*model.Issue{
		makeIssue("A", model.StatusOpen, nil, nil),
		makeIssue("B", model.StatusOpen, nil, []string{"A"}),
		makeIssue("C", model.StatusClosed, nil, nil),
	}
	issues[0].Blocks = []string{"B"}

	g := Build(issues)
	ready := g.Ready()

	if len(ready) != 1 || ready[0].ID != "A" {
		ids := make([]string, len(ready))
		for i, r := range ready {
			ids[i] = r.ID
		}
		t.Errorf("Ready() = %v, want [A]", ids)
	}
}

func TestBlocked(t *testing.T) {
	issues := []*model.Issue{
		makeIssue("A", model.StatusOpen, []string{"B"}, nil),
		makeIssue("B", model.StatusOpen, nil, []string{"A"}),
	}
	g := Build(issues)
	blocked := g.Blocked()

	if len(blocked) != 1 || blocked[0].ID != "B" {
		ids := make([]string, len(blocked))
		for i, r := range blocked {
			ids[i] = r.ID
		}
		t.Errorf("Blocked() = %v, want [B]", ids)
	}
}

func TestBlockedByClosedNotBlocked(t *testing.T) {
	issues := []*model.Issue{
		makeIssue("A", model.StatusClosed, []string{"B"}, nil),
		makeIssue("B", model.StatusOpen, nil, []string{"A"}),
	}
	g := Build(issues)
	blocked := g.Blocked()
	if len(blocked) != 0 {
		t.Errorf("B should not be blocked when A is closed, got %d blocked", len(blocked))
	}
}

func TestDetectCycles(t *testing.T) {
	issues := []*model.Issue{
		makeIssue("A", model.StatusOpen, []string{"B"}, nil),
		makeIssue("B", model.StatusOpen, []string{"C"}, []string{"A"}),
		makeIssue("C", model.StatusOpen, []string{"A"}, []string{"B"}),
	}
	g := Build(issues)
	cycles := g.DetectCycles()
	if len(cycles) == 0 {
		t.Error("expected at least one cycle")
	}
}

func TestBlockersOf(t *testing.T) {
	issues := []*model.Issue{
		makeIssue("A", model.StatusOpen, []string{"C"}, nil),
		makeIssue("B", model.StatusOpen, []string{"C"}, nil),
		makeIssue("C", model.StatusOpen, nil, []string{"A", "B"}),
	}
	g := Build(issues)
	blockers := g.BlockersOf("C")
	if len(blockers) != 2 {
		t.Errorf("expected 2 blockers of C, got %d", len(blockers))
	}
}

func TestDepTree(t *testing.T) {
	issues := []*model.Issue{
		makeIssue("A", model.StatusOpen, []string{"B", "C"}, nil),
		makeIssue("B", model.StatusOpen, []string{"D"}, []string{"A"}),
		makeIssue("C", model.StatusOpen, nil, []string{"A"}),
		makeIssue("D", model.StatusOpen, nil, []string{"B"}),
	}
	g := Build(issues)
	tree := g.DepTree("A")
	if tree == nil {
		t.Fatal("DepTree returned nil")
	}
	if tree.Issue.ID != "A" {
		t.Errorf("root = %s, want A", tree.Issue.ID)
	}
	if len(tree.Children) != 2 {
		t.Errorf("A should have 2 children, got %d", len(tree.Children))
	}
	// Find B child and check it has D.
	for _, child := range tree.Children {
		if child.Issue.ID == "B" {
			if len(child.Children) != 1 || child.Children[0].Issue.ID != "D" {
				t.Errorf("B should have 1 child D, got %v", child.Children)
			}
		}
	}
}

func TestStats(t *testing.T) {
	issues := []*model.Issue{
		makeIssue("A", model.StatusOpen, nil, nil),
		makeIssue("B", model.StatusClosed, nil, nil),
		makeIssue("C", model.StatusInProgress, nil, nil),
	}
	issues[1].Type = model.TypeBug
	g := Build(issues)
	s := g.Stats()
	if s.Total != 3 {
		t.Errorf("total = %d, want 3", s.Total)
	}
	if s.Open != 1 {
		t.Errorf("open = %d, want 1", s.Open)
	}
	if s.Closed != 1 {
		t.Errorf("closed = %d, want 1", s.Closed)
	}
	if s.InProgress != 1 {
		t.Errorf("in_progress = %d, want 1", s.InProgress)
	}
}
