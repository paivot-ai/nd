package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/paivot-ai/nd/internal/enforce"
	"github.com/paivot-ai/nd/internal/idgen"
	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/vlt"
	"gopkg.in/yaml.v3"
)

// CreateIssue generates an ID, serializes the issue to markdown, and writes it to the vault.
func (s *Store) CreateIssue(title, description, issueType string, priority int, assignee string, labels []string, parent string) (*model.Issue, error) {
	id, err := idgen.GenerateID(s.config.Prefix, title, s.IssueExists)
	if err != nil {
		return nil, fmt.Errorf("generate ID: %w", err)
	}
	return s.createIssue(id, title, description, issueType, priority, assignee, labels, parent)
}

// CreateIssueWithID creates an issue using a pre-determined ID (e.g. from import).
func (s *Store) CreateIssueWithID(id, title, description, issueType string, priority int, assignee string, labels []string, parent string) (*model.Issue, error) {
	return s.createIssue(id, title, description, issueType, priority, assignee, labels, parent)
}

func (s *Store) createIssue(id, title, description, issueType string, priority int, assignee string, labels []string, parent string) (*model.Issue, error) {
	itype, err := model.ParseIssueType(issueType)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	body := buildBody(description)
	issue := &model.Issue{
		ID:          id,
		Title:       title,
		Status:      model.StatusOpen,
		Priority:    model.Priority(priority),
		Type:        itype,
		Assignee:    assignee,
		Labels:      labels,
		Parent:      parent,
		CreatedAt:   now,
		CreatedBy:   s.config.CreatedBy,
		UpdatedAt:   now,
		ContentHash: enforce.ComputeContentHash(body),
		Body:        body,
	}

	if err := issue.ValidateWithCustom(s.CustomStatuses()); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	content := serializeIssue(issue)
	path := fmt.Sprintf("issues/%s.md", id)

	if err := s.vault.Create(id, path, content, true, false); err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}

	issue.FilePath = path

	// Populate Links section if the issue has relationships at creation time.
	if issue.Parent != "" || len(issue.Blocks) > 0 || len(issue.BlockedBy) > 0 || len(issue.Related) > 0 || len(issue.Follows) > 0 || len(issue.LedTo) > 0 {
		_ = s.UpdateLinksSection(id)
	}

	return issue, nil
}

// ReadIssue reads and deserializes an issue by ID.
func (s *Store) ReadIssue(id string) (*model.Issue, error) {
	res, err := s.vault.Read(id, "")
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", id, err)
	}
	issue, err := deserializeIssue(res.Content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", id, err)
	}
	issue.FilePath = fmt.Sprintf("issues/%s.md", id)
	return issue, nil
}

func buildBody(description string) string {
	var sb strings.Builder
	sb.WriteString("\n## Description\n")
	if description != "" {
		sb.WriteString(description)
		sb.WriteString("\n")
	}
	sb.WriteString("\n## Acceptance Criteria\n\n")
	sb.WriteString("\n## Design\n\n")
	sb.WriteString("\n## Notes\n\n")
	sb.WriteString("\n## History\n\n")
	sb.WriteString("\n## Links\n\n")
	sb.WriteString("\n## Comments\n")
	return sb.String()
}

// buildLinksSection generates wikilink lines from an issue's relationship fields.
// Returns empty string when no relationships exist.
func buildLinksSection(issue *model.Issue) string {
	var sb strings.Builder
	if issue.Parent != "" {
		sb.WriteString(fmt.Sprintf("- Parent: [[%s]]\n", issue.Parent))
	}
	if len(issue.Blocks) > 0 {
		links := make([]string, len(issue.Blocks))
		for i, b := range issue.Blocks {
			links[i] = fmt.Sprintf("[[%s]]", b)
		}
		sb.WriteString(fmt.Sprintf("- Blocks: %s\n", strings.Join(links, ", ")))
	}
	if len(issue.BlockedBy) > 0 {
		links := make([]string, len(issue.BlockedBy))
		for i, b := range issue.BlockedBy {
			links[i] = fmt.Sprintf("[[%s]]", b)
		}
		sb.WriteString(fmt.Sprintf("- Blocked by: %s\n", strings.Join(links, ", ")))
	}
	if len(issue.WasBlockedBy) > 0 {
		links := make([]string, len(issue.WasBlockedBy))
		for i, b := range issue.WasBlockedBy {
			links[i] = fmt.Sprintf("[[%s]]", b)
		}
		sb.WriteString(fmt.Sprintf("- Was blocked by: %s\n", strings.Join(links, ", ")))
	}
	if len(issue.Related) > 0 {
		links := make([]string, len(issue.Related))
		for i, r := range issue.Related {
			links[i] = fmt.Sprintf("[[%s]]", r)
		}
		sb.WriteString(fmt.Sprintf("- Related: %s\n", strings.Join(links, ", ")))
	}
	if len(issue.Follows) > 0 {
		links := make([]string, len(issue.Follows))
		for i, f := range issue.Follows {
			links[i] = fmt.Sprintf("[[%s]]", f)
		}
		sb.WriteString(fmt.Sprintf("- Follows: %s\n", strings.Join(links, ", ")))
	}
	if len(issue.LedTo) > 0 {
		links := make([]string, len(issue.LedTo))
		for i, l := range issue.LedTo {
			links[i] = fmt.Sprintf("[[%s]]", l)
		}
		sb.WriteString(fmt.Sprintf("- Led to: %s\n", strings.Join(links, ", ")))
	}
	return sb.String()
}

// serializeIssue converts an Issue to frontmatter + body markdown.
func serializeIssue(issue *model.Issue) string {
	fm := marshalFrontmatter(issue)
	return fmt.Sprintf("---\n%s---\n%s", fm, issue.Body)
}

func marshalFrontmatter(issue *model.Issue) string {
	// Use a map to control field ordering via manual construction.
	// yaml.Marshal on the struct would work but doesn't guarantee order.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("id: %s\n", issue.ID))
	sb.WriteString(fmt.Sprintf("title: %q\n", issue.Title))
	sb.WriteString(fmt.Sprintf("status: %s\n", issue.Status))
	sb.WriteString(fmt.Sprintf("priority: %d\n", issue.Priority))
	sb.WriteString(fmt.Sprintf("type: %s\n", issue.Type))
	if issue.Assignee != "" {
		sb.WriteString(fmt.Sprintf("assignee: %s\n", issue.Assignee))
	}
	if len(issue.Labels) > 0 {
		sb.WriteString(fmt.Sprintf("labels: [%s]\n", strings.Join(issue.Labels, ", ")))
	}
	if issue.Parent != "" {
		sb.WriteString(fmt.Sprintf("parent: %s\n", issue.Parent))
	}
	writeStringList(&sb, "blocks", issue.Blocks)
	writeStringList(&sb, "blocked_by", issue.BlockedBy)
	writeStringList(&sb, "was_blocked_by", issue.WasBlockedBy)
	writeStringList(&sb, "related", issue.Related)
	writeStringList(&sb, "follows", issue.Follows)
	writeStringList(&sb, "led_to", issue.LedTo)
	if issue.DeferUntil != "" {
		sb.WriteString(fmt.Sprintf("defer_until: %s\n", issue.DeferUntil))
	}
	sb.WriteString(fmt.Sprintf("created_at: %s\n", issue.CreatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("created_by: %s\n", issue.CreatedBy))
	sb.WriteString(fmt.Sprintf("updated_at: %s\n", issue.UpdatedAt.Format(time.RFC3339)))
	if issue.ClosedAt != "" {
		sb.WriteString(fmt.Sprintf("closed_at: %s\n", issue.ClosedAt))
	}
	if issue.CloseReason != "" {
		sb.WriteString(fmt.Sprintf("close_reason: %q\n", issue.CloseReason))
	}
	sb.WriteString(fmt.Sprintf("content_hash: %q\n", issue.ContentHash))
	return sb.String()
}

func writeStringList(sb *strings.Builder, key string, vals []string) {
	if len(vals) == 0 {
		return
	}
	sb.WriteString(fmt.Sprintf("%s: [%s]\n", key, strings.Join(vals, ", ")))
}

// DeleteIssue removes an issue, cleaning up all dependency references first.
// Returns the list of modified issue IDs (whose deps were cleaned up).
func (s *Store) DeleteIssue(id string, permanent bool) ([]string, error) {
	issue, err := s.ReadIssue(id)
	if err != nil {
		return nil, fmt.Errorf("issue %s: %w", id, err)
	}

	var modified []string

	// Clean up blocked_by references (issues that block this one).
	for _, depID := range issue.BlockedBy {
		if err := s.RemoveDependency(id, depID); err == nil {
			modified = append(modified, depID)
		}
	}

	// Clean up blocks references (issues this one blocks).
	for _, blockedID := range issue.Blocks {
		if err := s.RemoveDependency(blockedID, id); err == nil {
			modified = append(modified, blockedID)
		}
	}

	// Clean up follows references (predecessors that led to this issue).
	for _, predID := range issue.Follows {
		if pred, err := s.ReadIssue(predID); err == nil {
			newLedTo := remove(pred.LedTo, id)
			if err := s.setListProperty(predID, "led_to", newLedTo); err == nil {
				_ = s.UpdateLinksSection(predID)
				modified = append(modified, predID)
			}
		}
	}

	// Clean up led_to references (successors that follow this issue).
	for _, succID := range issue.LedTo {
		if succ, err := s.ReadIssue(succID); err == nil {
			newFollows := remove(succ.Follows, id)
			if err := s.setListProperty(succID, "follows", newFollows); err == nil {
				_ = s.UpdateLinksSection(succID)
				modified = append(modified, succID)
			}
		}
	}

	// Delete the file.
	path := fmt.Sprintf("issues/%s.md", id)
	if _, err := s.vault.Delete("", path, permanent); err != nil {
		return modified, fmt.Errorf("delete %s: %w", id, err)
	}

	return modified, nil
}

// ParseIssueMarkdown parses raw issue file content (frontmatter + body) into
// an Issue. Exported for consumers that read issue content outside a vault,
// such as the git sync merge engine.
func ParseIssueMarkdown(content string) (*model.Issue, error) {
	return deserializeIssue(content)
}

// SerializeIssueMarkdown renders an issue to frontmatter + body markdown.
func SerializeIssueMarkdown(issue *model.Issue) string {
	return serializeIssue(issue)
}

// BuildLinksSection generates the ## Links content from an issue's
// relationship fields. Empty when the issue has no relationships.
func BuildLinksSection(issue *model.Issue) string {
	return buildLinksSection(issue)
}

// deserializeIssue parses frontmatter + body markdown into an Issue.
func deserializeIssue(content string) (*model.Issue, error) {
	yamlStr, bodyStart, found := vlt.ExtractFrontmatter(content)
	if !found {
		return nil, fmt.Errorf("no frontmatter found")
	}

	var issue model.Issue
	if err := yaml.Unmarshal([]byte(yamlStr), &issue); err != nil {
		return nil, fmt.Errorf("unmarshal frontmatter: %w", err)
	}

	// Extract body: everything after the closing ---.
	lines := strings.SplitAfter(content, "\n")
	if bodyStart < len(lines) {
		issue.Body = strings.Join(lines[bodyStart:], "")
	}
	return &issue, nil
}
