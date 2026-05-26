package graph

import (
	"sort"

	"github.com/paivot-ai/nd/internal/model"
)

// PathNode represents an issue and its successors in an execution path tree.
type PathNode struct {
	Issue    *model.Issue
	Children []*PathNode
}

// ExecutionPath builds a tree from the given issue following LedTo edges forward.
func (g *Graph) ExecutionPath(id string) *PathNode {
	issue, ok := g.nodes[id]
	if !ok {
		return nil
	}
	visited := make(map[string]bool)
	return g.buildPathChildren(issue, visited)
}

func (g *Graph) buildPathChildren(issue *model.Issue, visited map[string]bool) *PathNode {
	if visited[issue.ID] {
		return &PathNode{Issue: issue}
	}
	visited[issue.ID] = true

	node := &PathNode{Issue: issue}
	for _, childID := range issue.LedTo {
		if child, ok := g.nodes[childID]; ok {
			node.Children = append(node.Children, g.buildPathChildren(child, visited))
		}
	}
	return node
}

// PathRoots returns issues that have LedTo edges but no Follows edges (start of chains).
func (g *Graph) PathRoots() []*model.Issue {
	var roots []*model.Issue
	for _, issue := range g.nodes {
		if len(issue.LedTo) > 0 && len(issue.Follows) == 0 {
			roots = append(roots, issue)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].ID < roots[j].ID
	})
	return roots
}
