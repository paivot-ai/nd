package format

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/RamXX/nd/internal/model"
)

// makeIssue creates a minimal issue for testing. All fields except ID, Title,
// Status, Priority, Type, Parent, and CreatedAt default to sensible values.
func makeIssue(id, title string, status model.Status, priority int, issueType model.IssueType, parent string) *model.Issue {
	return &model.Issue{
		ID:        id,
		Title:     title,
		Status:    status,
		Priority:  model.Priority(priority),
		Type:      issueType,
		Parent:    parent,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy: "tester",
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestFormatIssueLine(t *testing.T) {
	issue := makeIssue("TST-0001", "Fix the login bug", model.StatusOpen, 1, model.TypeBug, "")
	issue.Assignee = "alice"
	issue.Labels = []string{"auth", "urgent"}

	line := FormatIssueLine(issue)

	// Should contain the issue ID.
	if !strings.Contains(line, "TST-0001") {
		t.Errorf("line should contain issue ID: %s", line)
	}
	// Should contain assignee.
	if !strings.Contains(line, "@alice") {
		t.Errorf("line should contain assignee: %s", line)
	}
	// Should contain labels.
	if !strings.Contains(line, "auth, urgent") {
		t.Errorf("line should contain labels: %s", line)
	}
	// Should contain title.
	if !strings.Contains(line, "Fix the login bug") {
		t.Errorf("line should contain title: %s", line)
	}
}

func TestFormatIssueLine_Closed(t *testing.T) {
	issue := makeIssue("TST-0002", "Old task", model.StatusClosed, 2, model.TypeTask, "")

	line := FormatIssueLine(issue)

	// Closed issues should contain the closed icon.
	if !strings.Contains(line, "TST-0002") {
		t.Errorf("closed line should contain issue ID: %s", line)
	}
}

func TestFormatIssueLine_TruncatesLongTitle(t *testing.T) {
	longTitle := strings.Repeat("A", 80)
	issue := makeIssue("TST-0003", longTitle, model.StatusOpen, 1, model.TypeTask, "")

	line := FormatIssueLine(issue)

	if !strings.Contains(line, "...") {
		t.Errorf("long title should be truncated with ...: %s", line)
	}
}

func TestTree_BasicGrouping(t *testing.T) {
	epic := makeIssue("TST-epic", "Auth Epic", model.StatusOpen, 1, model.TypeEpic, "")
	child1 := makeIssue("TST-ch01", "Design auth", model.StatusOpen, 1, model.TypeTask, "TST-epic")
	child2 := makeIssue("TST-ch02", "Implement auth", model.StatusInProgress, 1, model.TypeFeature, "TST-epic")

	issues := []*model.Issue{epic, child1, child2}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", false)
	output := buf.String()

	// Parent should appear without connector.
	if !strings.Contains(output, "TST-epic") {
		t.Errorf("output should contain parent epic: %s", output)
	}
	// Children should appear with tree connectors.
	if !strings.Contains(output, "├── ") && !strings.Contains(output, "└── ") {
		t.Errorf("output should contain tree connectors: %s", output)
	}
	if !strings.Contains(output, "TST-ch01") {
		t.Errorf("output should contain child1: %s", output)
	}
	if !strings.Contains(output, "TST-ch02") {
		t.Errorf("output should contain child2: %s", output)
	}
	// Count should include all 3 issues.
	if !strings.Contains(output, "3 issue(s)") {
		t.Errorf("count should be 3: %s", output)
	}
}

func TestTree_Unparented(t *testing.T) {
	task1 := makeIssue("TST-t001", "Standalone task", model.StatusOpen, 2, model.TypeTask, "")
	task2 := makeIssue("TST-t002", "Another task", model.StatusOpen, 3, model.TypeChore, "")

	issues := []*model.Issue{task1, task2}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", false)
	output := buf.String()

	// Should have [Unparented] section.
	if !strings.Contains(output, "[Unparented]") {
		t.Errorf("output should contain [Unparented] section: %s", output)
	}
	if !strings.Contains(output, "TST-t001") {
		t.Errorf("output should contain task1: %s", output)
	}
	if !strings.Contains(output, "TST-t002") {
		t.Errorf("output should contain task2: %s", output)
	}
	if !strings.Contains(output, "2 issue(s)") {
		t.Errorf("count should be 2: %s", output)
	}
}

func TestTree_ContextOnly(t *testing.T) {
	// Context-only parent: excluded by filters, fetched for display only.
	epic := makeIssue("TST-epic", "Auth Epic", model.StatusClosed, 1, model.TypeEpic, "")
	child1 := makeIssue("TST-ch01", "Design auth", model.StatusOpen, 1, model.TypeTask, "TST-epic")

	issues := []*model.Issue{epic, child1}
	contextIDs := map[string]bool{"TST-epic": true}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", false)
	output := buf.String()

	// Context-only epic should still appear (for grouping).
	if !strings.Contains(output, "TST-epic") {
		t.Errorf("output should contain context-only epic: %s", output)
	}
	// Count should exclude context-only parent (only child1 counts).
	if !strings.Contains(output, "1 issue(s)") {
		t.Errorf("count should be 1 (context parent excluded): %s", output)
	}
}

func TestTree_EmptyList(t *testing.T) {
	var buf bytes.Buffer
	Tree(&buf, nil, map[string]bool{}, "priority", false)
	output := buf.String()

	if !strings.Contains(output, "No issues found.") {
		t.Errorf("empty list should show 'No issues found.': %s", output)
	}
}

func TestTree_DeepNesting(t *testing.T) {
	// 3+ levels: epic -> feature -> subtask.
	epic := makeIssue("TST-epic", "Top Epic", model.StatusOpen, 1, model.TypeEpic, "")
	feature := makeIssue("TST-feat", "Feature under epic", model.StatusOpen, 1, model.TypeFeature, "TST-epic")
	subtask := makeIssue("TST-sub1", "Subtask under feature", model.StatusOpen, 1, model.TypeTask, "TST-feat")

	issues := []*model.Issue{epic, feature, subtask}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", false)
	output := buf.String()

	// All three should appear.
	if !strings.Contains(output, "TST-epic") {
		t.Errorf("output should contain epic: %s", output)
	}
	if !strings.Contains(output, "TST-feat") {
		t.Errorf("output should contain feature: %s", output)
	}
	if !strings.Contains(output, "TST-sub1") {
		t.Errorf("output should contain subtask: %s", output)
	}
	// Should have nested connectors (the subtask is indented deeper).
	if !strings.Contains(output, "3 issue(s)") {
		t.Errorf("count should be 3: %s", output)
	}
}

func TestTree_SortWithinGroups(t *testing.T) {
	epic := makeIssue("TST-epic", "Top Epic", model.StatusOpen, 1, model.TypeEpic, "")
	child_p3 := makeIssue("TST-ch01", "Low priority child", model.StatusOpen, 3, model.TypeTask, "TST-epic")
	child_p1 := makeIssue("TST-ch02", "High priority child", model.StatusOpen, 1, model.TypeTask, "TST-epic")
	child_p2 := makeIssue("TST-ch03", "Medium priority child", model.StatusOpen, 2, model.TypeTask, "TST-epic")

	issues := []*model.Issue{epic, child_p3, child_p1, child_p2}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", false)
	output := buf.String()

	// Children should be sorted by priority: P1, P2, P3.
	ch02Idx := strings.Index(output, "TST-ch02")
	ch03Idx := strings.Index(output, "TST-ch03")
	ch01Idx := strings.Index(output, "TST-ch01")

	if ch02Idx < 0 || ch03Idx < 0 || ch01Idx < 0 {
		t.Fatalf("all children should appear in output: %s", output)
	}
	if ch02Idx > ch03Idx {
		t.Errorf("P1 child (TST-ch02) should come before P2 child (TST-ch03)")
	}
	if ch03Idx > ch01Idx {
		t.Errorf("P2 child (TST-ch03) should come before P3 child (TST-ch01)")
	}
}

func TestTree_SortWithinGroupsReverse(t *testing.T) {
	epic := makeIssue("TST-epic", "Top Epic", model.StatusOpen, 1, model.TypeEpic, "")
	child_p1 := makeIssue("TST-ch01", "High priority", model.StatusOpen, 1, model.TypeTask, "TST-epic")
	child_p3 := makeIssue("TST-ch02", "Low priority", model.StatusOpen, 3, model.TypeTask, "TST-epic")

	issues := []*model.Issue{epic, child_p1, child_p3}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", true)
	output := buf.String()

	// With reverse, P3 should come before P1.
	ch01Idx := strings.Index(output, "TST-ch01")
	ch02Idx := strings.Index(output, "TST-ch02")

	if ch01Idx < 0 || ch02Idx < 0 {
		t.Fatalf("both children should appear: %s", output)
	}
	if ch02Idx > ch01Idx {
		t.Errorf("with reverse, P3 child (TST-ch02) should come before P1 child (TST-ch01)")
	}
}

func TestTree_MixedParentsAndUnparented(t *testing.T) {
	epic := makeIssue("TST-epic", "Auth Epic", model.StatusOpen, 1, model.TypeEpic, "")
	child := makeIssue("TST-ch01", "Design auth", model.StatusOpen, 1, model.TypeTask, "TST-epic")
	orphan := makeIssue("TST-orph", "Standalone task", model.StatusOpen, 2, model.TypeTask, "")

	issues := []*model.Issue{epic, child, orphan}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", false)
	output := buf.String()

	// Epic and child should be grouped.
	if !strings.Contains(output, "TST-epic") {
		t.Errorf("output should contain epic: %s", output)
	}
	if !strings.Contains(output, "TST-ch01") {
		t.Errorf("output should contain child: %s", output)
	}
	// Orphan should be in [Unparented].
	if !strings.Contains(output, "[Unparented]") {
		t.Errorf("output should contain [Unparented] section: %s", output)
	}
	if !strings.Contains(output, "TST-orph") {
		t.Errorf("output should contain orphan: %s", output)
	}
	// Count should be all 3.
	if !strings.Contains(output, "3 issue(s)") {
		t.Errorf("count should be 3: %s", output)
	}
}

func TestTree_NoUnparentedSectionWhenEmpty(t *testing.T) {
	epic := makeIssue("TST-epic", "Auth Epic", model.StatusOpen, 1, model.TypeEpic, "")
	child := makeIssue("TST-ch01", "Task", model.StatusOpen, 1, model.TypeTask, "TST-epic")

	issues := []*model.Issue{epic, child}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "priority", false)
	output := buf.String()

	if strings.Contains(output, "[Unparented]") {
		t.Errorf("output should NOT contain [Unparented] when all issues have parents: %s", output)
	}
}

func TestTree_ConnectorPositions(t *testing.T) {
	epic := makeIssue("TST-epic", "Epic", model.StatusOpen, 1, model.TypeEpic, "")
	child1 := makeIssue("TST-ch01", "First child", model.StatusOpen, 1, model.TypeTask, "TST-epic")
	child2 := makeIssue("TST-ch02", "Second child", model.StatusOpen, 1, model.TypeTask, "TST-epic")
	child3 := makeIssue("TST-ch03", "Third child", model.StatusOpen, 1, model.TypeTask, "TST-epic")

	issues := []*model.Issue{epic, child1, child2, child3}
	contextIDs := map[string]bool{}

	var buf bytes.Buffer
	Tree(&buf, issues, contextIDs, "id", false)
	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	// Find child lines (lines containing tree connectors).
	var childLines []string
	for _, line := range lines {
		if strings.Contains(line, "├── ") || strings.Contains(line, "└── ") {
			childLines = append(childLines, line)
		}
	}

	if len(childLines) < 3 {
		t.Fatalf("expected at least 3 child lines with connectors, got %d: %v", len(childLines), childLines)
	}

	// First and second children should use ├──, last should use └──.
	if !strings.Contains(childLines[0], "├── ") {
		t.Errorf("first child should use ├──: %s", childLines[0])
	}
	if !strings.Contains(childLines[1], "├── ") {
		t.Errorf("second child should use ├──: %s", childLines[1])
	}
	if !strings.Contains(childLines[2], "└── ") {
		t.Errorf("last child should use └──: %s", childLines[2])
	}
}

func TestTable_UnchangedBehavior(t *testing.T) {
	issue1 := makeIssue("TST-0001", "First issue", model.StatusOpen, 1, model.TypeBug, "")
	issue2 := makeIssue("TST-0002", "Second issue", model.StatusClosed, 2, model.TypeTask, "")

	issues := []*model.Issue{issue1, issue2}

	var buf bytes.Buffer
	Table(&buf, issues)
	output := buf.String()

	if !strings.Contains(output, "TST-0001") {
		t.Errorf("Table output should contain first issue: %s", output)
	}
	if !strings.Contains(output, "TST-0002") {
		t.Errorf("Table output should contain second issue: %s", output)
	}
	if !strings.Contains(output, "2 issue(s)") {
		t.Errorf("Table output should show count: %s", output)
	}
}

func TestTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	Table(&buf, nil)
	output := buf.String()

	if !strings.Contains(output, "No issues found.") {
		t.Errorf("Table with no issues should show 'No issues found.': %s", output)
	}
}
