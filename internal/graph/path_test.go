package graph

import (
	"testing"

	"github.com/paivot-ai/nd/internal/model"
)

func makePathIssue(id string, status model.Status, ledTo, follows []string) *model.Issue {
	return &model.Issue{
		ID:        id,
		Title:     "Issue " + id,
		Status:    status,
		Priority:  2,
		Type:      model.TypeTask,
		CreatedAt: makeIssue("", model.StatusOpen, nil, nil).CreatedAt,
		CreatedBy: "test",
		LedTo:     ledTo,
		Follows:   follows,
	}
}

func TestExecutionPathChain(t *testing.T) {
	issues := []*model.Issue{
		makePathIssue("A", model.StatusClosed, []string{"B"}, nil),
		makePathIssue("B", model.StatusClosed, []string{"C"}, []string{"A"}),
		makePathIssue("C", model.StatusInProgress, nil, []string{"B"}),
	}
	g := Build(issues)
	tree := g.ExecutionPath("A")
	if tree == nil {
		t.Fatal("ExecutionPath returned nil")
	}
	if tree.Issue.ID != "A" {
		t.Errorf("root = %s, want A", tree.Issue.ID)
	}
	if len(tree.Children) != 1 || tree.Children[0].Issue.ID != "B" {
		t.Fatalf("A should have 1 child B, got %d children", len(tree.Children))
	}
	bNode := tree.Children[0]
	if len(bNode.Children) != 1 || bNode.Children[0].Issue.ID != "C" {
		t.Errorf("B should have 1 child C, got %d children", len(bNode.Children))
	}
}

func TestPathRoots(t *testing.T) {
	issues := []*model.Issue{
		makePathIssue("A", model.StatusClosed, []string{"B"}, nil),           // root: has LedTo, no Follows
		makePathIssue("B", model.StatusClosed, []string{"C"}, []string{"A"}), // not root: has Follows
		makePathIssue("C", model.StatusInProgress, nil, []string{"B"}),       // not root: no LedTo
		makePathIssue("D", model.StatusOpen, nil, nil),                       // not root: no LedTo
	}
	g := Build(issues)
	roots := g.PathRoots()
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0].ID != "A" {
		t.Errorf("root = %s, want A", roots[0].ID)
	}
}

func TestExecutionPathCycle(t *testing.T) {
	issues := []*model.Issue{
		makePathIssue("A", model.StatusClosed, []string{"B"}, []string{"B"}),
		makePathIssue("B", model.StatusClosed, []string{"A"}, []string{"A"}),
	}
	g := Build(issues)
	// Should not infinite loop.
	tree := g.ExecutionPath("A")
	if tree == nil {
		t.Fatal("ExecutionPath returned nil on cycle")
	}
	if tree.Issue.ID != "A" {
		t.Errorf("root = %s, want A", tree.Issue.ID)
	}
}

func TestExecutionPathDisconnected(t *testing.T) {
	issues := []*model.Issue{
		makePathIssue("A", model.StatusOpen, nil, nil),
	}
	g := Build(issues)
	tree := g.ExecutionPath("A")
	if tree == nil {
		t.Fatal("ExecutionPath returned nil")
	}
	if tree.Issue.ID != "A" {
		t.Errorf("root = %s, want A", tree.Issue.ID)
	}
	if len(tree.Children) != 0 {
		t.Errorf("disconnected issue should have 0 children, got %d", len(tree.Children))
	}
}
