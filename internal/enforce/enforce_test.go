package enforce

import (
	"strings"
	"testing"
	"time"

	"github.com/paivot-ai/nd/internal/model"
)

func TestComputeContentHash(t *testing.T) {
	hash := ComputeContentHash("hello world")
	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("hash should start with sha256:, got %s", hash)
	}
	// Same input should produce same hash.
	hash2 := ComputeContentHash("hello world")
	if hash != hash2 {
		t.Error("same input should produce same hash")
	}
	// Different input should produce different hash.
	hash3 := ComputeContentHash("different")
	if hash == hash3 {
		t.Error("different input should produce different hash")
	}
}

func TestValidateIssue(t *testing.T) {
	good := &model.Issue{
		ID:        "X-001",
		Title:     "Test",
		Status:    model.StatusOpen,
		Priority:  2,
		Type:      model.TypeTask,
		CreatedAt: time.Now(),
		CreatedBy: "tester",
	}
	if err := ValidateIssue(good); err != nil {
		t.Errorf("valid issue: %v", err)
	}
}

func TestValidateDeps(t *testing.T) {
	issue := &model.Issue{
		ID:     "X-001",
		Blocks: []string{"X-001"},
	}
	if err := ValidateDeps(issue); err == nil {
		t.Error("self-blocking should be an error")
	}

	issue2 := &model.Issue{
		ID:        "X-002",
		BlockedBy: []string{"X-002"},
	}
	if err := ValidateDeps(issue2); err == nil {
		t.Error("self-blocked should be an error")
	}

	issue3 := &model.Issue{
		ID:     "X-003",
		Blocks: []string{"X-004"},
	}
	if err := ValidateDeps(issue3); err != nil {
		t.Errorf("valid deps: %v", err)
	}
}
