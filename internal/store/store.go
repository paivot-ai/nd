package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RamXX/nd/internal/model"
	"github.com/RamXX/vlt"
	"gopkg.in/yaml.v3"
)

// gitignoreEntries are the lines that must appear in the vault's .gitignore.
// Blank lines and comments are included for readability.
var gitignoreEntries = []string{
	"# nd runtime state -- do not track",
	"# Issues are the system of record; use `nd archive` for git-committable snapshots",
	".nd.yaml",
	".vlt.lock",
	".trash/",
	"issues/",
	".guard/",
	".piv-loop-state.json",
	".piv-loop-snapshot.json",
	".dispatcher-state.json",
}

// EnsureGitignore idempotently adds any missing entries to the vault's .gitignore.
// It creates the file if it does not exist. Safe to call on every Open.
func (s *Store) EnsureGitignore() error {
	return ensureGitignore(s.dir)
}

func ensureGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")

	existing := make(map[string]bool)
	f, err := os.Open(path)
	if err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			existing[scanner.Text()] = true
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan .gitignore: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("open .gitignore: %w", err)
	}

	var missing []string
	for _, entry := range gitignoreEntries {
		if !existing[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil // nothing to do
	}

	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open .gitignore for append: %w", err)
	}
	defer out.Close()

	// Add a newline separator if the file already had content.
	if len(existing) > 0 {
		if _, err := out.WriteString("\n"); err != nil {
			return err
		}
	}
	for _, entry := range missing {
		if _, err := out.WriteString(entry + "\n"); err != nil {
			return err
		}
	}
	return nil
}

// Config holds vault-level nd configuration stored in .nd.yaml.
type Config struct {
	Version         string `yaml:"version"`
	Prefix          string `yaml:"prefix"`
	CreatedBy       string `yaml:"created_by"`
	StatusCustom    string `yaml:"status_custom,omitempty"`
	StatusSequence  string `yaml:"status_sequence,omitempty"`
	StatusFSM       bool   `yaml:"status_fsm,omitempty"`
	StatusExitRules string `yaml:"status_exit_rules,omitempty"`
}

// Store wraps a vlt.Vault with issue-tracker operations.
// All operations are protected by an advisory file lock acquired at Open.
// Callers must call Close() when done to release the lock.
type Store struct {
	vault  *vlt.Vault
	config Config
	dir    string
	unlock func() // releases the advisory file lock; nil if not locked
}

// Open opens an existing nd vault at dir, acquiring an exclusive advisory lock.
// The lock is held until Close() is called.
func Open(dir string) (*Store, error) {
	unlock, err := vlt.LockVault(dir, true)
	if err != nil {
		return nil, fmt.Errorf("lock vault: %w", err)
	}

	v, err := vlt.Open(dir)
	if err != nil {
		unlock()
		return nil, fmt.Errorf("open vault: %w", err)
	}
	s := &Store{vault: v, dir: dir, unlock: unlock}
	if err := s.loadConfig(); err != nil {
		unlock()
		return nil, fmt.Errorf("load config: %w", err)
	}
	// Ensure existing vaults have a complete .gitignore.
	_ = s.EnsureGitignore()
	return s, nil
}

// Close releases the advisory file lock. Safe to call multiple times.
func (s *Store) Close() {
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
}

// Init creates a new nd vault at dir.
func Init(dir, prefix, author string) (*Store, error) {
	// Create vault directory structure.
	for _, sub := range []string{"issues", ".trash"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}

	// Write .gitignore (idempotent -- appends to any existing file).
	if err := ensureGitignore(dir); err != nil {
		return nil, fmt.Errorf("write .gitignore: %w", err)
	}

	// Write config.
	cfg := Config{
		Version:   "1",
		Prefix:    prefix,
		CreatedBy: author,
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".nd.yaml"), data, 0o644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	v, err := vlt.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open vault after init: %w", err)
	}
	return &Store{vault: v, config: cfg, dir: dir}, nil
}

func (s *Store) loadConfig() error {
	data, err := os.ReadFile(filepath.Join(s.dir, ".nd.yaml"))
	if err != nil {
		return fmt.Errorf("read .nd.yaml: %w", err)
	}
	return yaml.Unmarshal(data, &s.config)
}

// Vault returns the underlying vlt.Vault for direct operations.
func (s *Store) Vault() *vlt.Vault { return s.vault }

// Config returns the nd configuration.
func (s *Store) Config() Config { return s.config }

// Dir returns the vault root directory.
func (s *Store) Dir() string { return s.dir }

// Prefix returns the configured issue ID prefix.
func (s *Store) Prefix() string { return s.config.Prefix }

// IssueExists checks whether an issue with the given ID exists in the vault.
func (s *Store) IssueExists(id string) bool {
	p := filepath.Join(s.dir, "issues", id+".md")
	_, err := os.Stat(p)
	return err == nil
}

// CustomStatuses parses the status_custom config field into a slice of model.Status.
func (s *Store) CustomStatuses() []model.Status {
	return parseCSVStatuses(s.config.StatusCustom)
}

// StatusSequence parses the status_sequence config field into an ordered slice.
func (s *Store) StatusSequence() []model.Status {
	return parseCSVStatuses(s.config.StatusSequence)
}

// ExitRules parses the status_exit_rules config into a map of status -> allowed exit targets.
// Format: "blocked:open,in_progress,deferred;deferred:open,in_progress,deferred"
func (s *Store) ExitRules() map[model.Status][]model.Status {
	return parseExitRules(s.config.StatusExitRules)
}

func parseExitRules(raw string) map[model.Status][]model.Status {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	result := make(map[model.Status][]model.Status)
	for _, rule := range strings.Split(raw, ";") {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		parts := strings.SplitN(rule, ":", 2)
		if len(parts) != 2 {
			continue
		}
		from := model.Status(strings.TrimSpace(strings.ToLower(parts[0])))
		targets := parseCSVStatuses(parts[1])
		if len(targets) > 0 {
			result[from] = targets
		}
	}
	return result
}

func parseCSVStatuses(csv string) []model.Status {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]model.Status, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, model.Status(strings.ToLower(p)))
		}
	}
	return out
}

// SaveConfig writes the current config back to .nd.yaml.
func (s *Store) SaveConfig() error {
	data, err := yaml.Marshal(s.config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(filepath.Join(s.dir, ".nd.yaml"), data, 0o644)
}

var validConfigKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// SetConfigValue sets a config field by dot-notation key with validation.
func (s *Store) SetConfigValue(key, value string) error {
	switch key {
	case "status.custom":
		if value != "" {
			for _, name := range strings.Split(value, ",") {
				name = strings.TrimSpace(strings.ToLower(name))
				if name == "" {
					continue
				}
				if !validConfigKeyRe.MatchString(name) {
					return fmt.Errorf("invalid custom status name %q: must be lowercase alphanumeric/underscore", name)
				}
				if model.IsBuiltinStatus(name) {
					return fmt.Errorf("custom status %q collides with built-in status", name)
				}
			}
		}
		s.config.StatusCustom = value

	case "status.sequence":
		if value != "" {
			custom := s.CustomStatuses()
			seen := make(map[string]bool)
			for _, name := range strings.Split(value, ",") {
				name = strings.TrimSpace(strings.ToLower(name))
				if name == "" {
					continue
				}
				if seen[name] {
					return fmt.Errorf("duplicate status %q in sequence", name)
				}
				seen[name] = true
				if !model.IsBuiltinStatus(name) {
					found := false
					for _, c := range custom {
						if string(c) == name {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("status %q in sequence is not a built-in or custom status", name)
					}
				}
			}
		}
		s.config.StatusSequence = value

	case "status.fsm":
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			if s.config.StatusSequence == "" {
				return fmt.Errorf("cannot enable FSM without a status sequence; set status.sequence first")
			}
			s.config.StatusFSM = true
		case "false", "0", "no":
			s.config.StatusFSM = false
		default:
			return fmt.Errorf("invalid boolean value %q for status.fsm", value)
		}

	case "status.exit_rules":
		if value != "" {
			custom := s.CustomStatuses()
			for _, rule := range strings.Split(value, ";") {
				rule = strings.TrimSpace(rule)
				if rule == "" {
					continue
				}
				parts := strings.SplitN(rule, ":", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid exit rule %q: expected format status:target1,target2", rule)
				}
				fromName := strings.TrimSpace(strings.ToLower(parts[0]))
				if _, err := model.ParseStatusWithCustom(fromName, custom); err != nil {
					return fmt.Errorf("invalid status %q in exit rule: %w", fromName, err)
				}
				for _, target := range strings.Split(parts[1], ",") {
					target = strings.TrimSpace(strings.ToLower(target))
					if target == "" {
						continue
					}
					if _, err := model.ParseStatusWithCustom(target, custom); err != nil {
						return fmt.Errorf("invalid target %q in exit rule for %s: %w", target, fromName, err)
					}
				}
			}
		}
		s.config.StatusExitRules = value

	default:
		return fmt.Errorf("unknown config key %q", key)
	}

	return s.SaveConfig()
}

// GetConfigValue returns the value of a config field by dot-notation key.
func (s *Store) GetConfigValue(key string) (string, error) {
	switch key {
	case "version":
		return s.config.Version, nil
	case "prefix":
		return s.config.Prefix, nil
	case "created_by":
		return s.config.CreatedBy, nil
	case "status.custom":
		return s.config.StatusCustom, nil
	case "status.sequence":
		return s.config.StatusSequence, nil
	case "status.fsm":
		if s.config.StatusFSM {
			return "true", nil
		}
		return "false", nil
	case "status.exit_rules":
		return s.config.StatusExitRules, nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

// ConfigEntries returns all config fields as key-value pairs for listing.
func (s *Store) ConfigEntries() [][2]string {
	fsm := "false"
	if s.config.StatusFSM {
		fsm = "true"
	}
	return [][2]string{
		{"version", s.config.Version},
		{"prefix", s.config.Prefix},
		{"created_by", s.config.CreatedBy},
		{"status.custom", s.config.StatusCustom},
		{"status.sequence", s.config.StatusSequence},
		{"status.fsm", fsm},
		{"status.exit_rules", s.config.StatusExitRules},
	}
}
