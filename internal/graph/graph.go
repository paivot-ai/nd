package graph

import "github.com/paivot-ai/nd/internal/model"

// Graph is an in-memory dependency graph built from a slice of issues.
type Graph struct {
	nodes   map[string]*model.Issue
	forward map[string][]string // blocks: A -> [B, C] means A blocks B, C
	reverse map[string][]string // blocked_by: A -> [B, C] means A is blocked by B, C
}

// Build constructs a dependency graph from a list of issues.
func Build(issues []*model.Issue) *Graph {
	g := &Graph{
		nodes:   make(map[string]*model.Issue, len(issues)),
		forward: make(map[string][]string),
		reverse: make(map[string][]string),
	}
	for _, issue := range issues {
		g.nodes[issue.ID] = issue
		for _, b := range issue.Blocks {
			g.forward[issue.ID] = append(g.forward[issue.ID], b)
		}
		for _, b := range issue.BlockedBy {
			g.reverse[issue.ID] = append(g.reverse[issue.ID], b)
		}
	}
	return g
}

// Ready returns issues that are actionable: open or in_progress with no open blockers.
func (g *Graph) Ready() []*model.Issue {
	var ready []*model.Issue
	for _, issue := range g.nodes {
		if issue.Status == model.StatusClosed || issue.Status == model.StatusDeferred {
			continue
		}
		if g.hasOpenBlockers(issue) {
			continue
		}
		ready = append(ready, issue)
	}
	return ready
}

// Blocked returns issues that have at least one open blocker.
func (g *Graph) Blocked() []*model.Issue {
	var blocked []*model.Issue
	for _, issue := range g.nodes {
		if issue.Status == model.StatusClosed {
			continue
		}
		if g.hasOpenBlockers(issue) {
			blocked = append(blocked, issue)
		}
	}
	return blocked
}

// BlockersOf returns the open issues blocking the given issue ID.
func (g *Graph) BlockersOf(id string) []*model.Issue {
	var blockers []*model.Issue
	for _, depID := range g.reverse[id] {
		if dep, ok := g.nodes[depID]; ok && dep.IsOpen() {
			blockers = append(blockers, dep)
		}
	}
	return blockers
}

// hasOpenBlockers checks if any of the issue's blockers are still open.
func (g *Graph) hasOpenBlockers(issue *model.Issue) bool {
	for _, depID := range issue.BlockedBy {
		if dep, ok := g.nodes[depID]; ok && dep.IsOpen() {
			return true
		}
	}
	return false
}

// DetectCycles finds dependency cycles using DFS.
// Returns a list of cycle paths (each path is a slice of IDs forming the cycle).
func (g *Graph) DetectCycles() [][]string {
	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	var cycles [][]string
	var path []string

	var dfs func(id string)
	dfs = func(id string) {
		if onStack[id] {
			// Found a cycle -- extract it.
			cycle := []string{id}
			for i := len(path) - 1; i >= 0; i-- {
				cycle = append(cycle, path[i])
				if path[i] == id {
					break
				}
			}
			cycles = append(cycles, cycle)
			return
		}
		if visited[id] {
			return
		}
		visited[id] = true
		onStack[id] = true
		path = append(path, id)

		for _, next := range g.forward[id] {
			dfs(next)
		}

		path = path[:len(path)-1]
		onStack[id] = false
	}

	for id := range g.nodes {
		dfs(id)
	}
	return cycles
}

// DepNode represents an issue and its dependents in a dependency tree.
type DepNode struct {
	Issue    *model.Issue
	Children []*DepNode
}

// DepTree builds a dependency tree rooted at the given issue ID.
// Children are issues blocked by the root (i.e., forward edges).
func (g *Graph) DepTree(id string) *DepNode {
	issue, ok := g.nodes[id]
	if !ok {
		return nil
	}
	visited := make(map[string]bool)
	return g.buildDepChildren(issue, visited)
}

func (g *Graph) buildDepChildren(issue *model.Issue, visited map[string]bool) *DepNode {
	if visited[issue.ID] {
		return &DepNode{Issue: issue}
	}
	visited[issue.ID] = true

	node := &DepNode{Issue: issue}
	for _, childID := range g.forward[issue.ID] {
		if child, ok := g.nodes[childID]; ok {
			node.Children = append(node.Children, g.buildDepChildren(child, visited))
		}
	}
	return node
}

// Stats returns aggregate counts.
type Stats struct {
	Total      int
	Open       int
	InProgress int
	Blocked    int
	Closed     int
	Deferred   int
	ByType     map[string]int
	ByPriority map[int]int
	ByStatus   map[string]int
}

func (g *Graph) Stats() Stats {
	s := Stats{
		ByType:     make(map[string]int),
		ByPriority: make(map[int]int),
		ByStatus:   make(map[string]int),
	}
	for _, issue := range g.nodes {
		s.Total++
		s.ByStatus[string(issue.Status)]++
		switch issue.Status {
		case model.StatusOpen:
			s.Open++
		case model.StatusInProgress:
			s.InProgress++
		case model.StatusBlocked:
			s.Blocked++
		case model.StatusClosed:
			s.Closed++
		case model.StatusDeferred:
			s.Deferred++
		}
		s.ByType[string(issue.Type)]++
		s.ByPriority[int(issue.Priority)]++
	}
	// Also count dynamically blocked (open blockers).
	for _, issue := range g.nodes {
		if issue.Status != model.StatusClosed && g.hasOpenBlockers(issue) && issue.Status != model.StatusBlocked {
			s.Blocked++
		}
	}
	return s
}
