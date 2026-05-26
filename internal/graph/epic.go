package graph

import (
	"github.com/paivot-ai/nd/internal/model"
)

// EpicNode represents an epic and its children in a tree.
type EpicNode struct {
	Issue    *model.Issue
	Children []*EpicNode
}

// EpicSummary contains aggregate stats for an epic and its children.
type EpicSummary struct {
	Epic       *model.Issue
	Total      int
	Open       int
	InProgress int
	Closed     int
	Blocked    int
}

// EpicTree builds a tree rooted at the given epic ID.
// Children are found by matching Parent field or by dot-notation IDs (e.g., EPIC.1, EPIC.2).
func (g *Graph) EpicTree(epicID string) *EpicNode {
	epic, ok := g.nodes[epicID]
	if !ok {
		return nil
	}

	node := &EpicNode{Issue: epic}
	g.buildChildren(node, epicID)
	return node
}

func (g *Graph) buildChildren(parent *EpicNode, parentID string) {
	for _, issue := range g.nodes {
		if issue.Parent == parentID {
			child := &EpicNode{Issue: issue}
			g.buildChildren(child, issue.ID)
			parent.Children = append(parent.Children, child)
		}
	}
}

// EpicStatus computes aggregate statistics for an epic and all its descendants.
func (g *Graph) EpicStatus(epicID string) *EpicSummary {
	tree := g.EpicTree(epicID)
	if tree == nil {
		return nil
	}

	summary := &EpicSummary{Epic: tree.Issue}
	g.countTree(tree, summary)
	return summary
}

func (g *Graph) countTree(node *EpicNode, summary *EpicSummary) {
	// Don't count the epic itself, only children.
	for _, child := range node.Children {
		summary.Total++
		switch child.Issue.Status {
		case model.StatusOpen:
			summary.Open++
		case model.StatusInProgress:
			summary.InProgress++
		case model.StatusClosed:
			summary.Closed++
		case model.StatusBlocked:
			summary.Blocked++
		}
		if g.hasOpenBlockers(child.Issue) && child.Issue.Status != model.StatusBlocked {
			summary.Blocked++
		}
		g.countTree(child, summary)
	}
}

// Epics returns all issues of type epic.
func (g *Graph) Epics() []*model.Issue {
	var epics []*model.Issue
	for _, issue := range g.nodes {
		if issue.Type == model.TypeEpic {
			epics = append(epics, issue)
		}
	}
	return epics
}
