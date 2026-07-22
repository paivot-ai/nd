package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/paivot-ai/nd/internal/model"
	"github.com/paivot-ai/vlt"
	"gopkg.in/yaml.v3"
)

// gitignoreEntries returns the lines that must appear in the vault's .gitignore.
// Blank lines and comments are included for readability.
func gitignoreEntries(trackIssues bool) []string {
	entries := []string{
		"# nd runtime state -- do not track",
	}
	if trackIssues {
		entries = append(entries, "# Tracked mode keeps .nd.yaml and issues/ in git")
	} else {
		entries = append(entries, "# Default mode keeps .nd.yaml and issues/ out of code branches; `nd sync` persists them on the backlog branch")
		entries = append(entries, ".nd.yaml", "issues/")
	}
	return append(entries,
		".vlt.lock",
		".trash/",
		".guard/",
		".piv-loop-state.json",
		".piv-loop-snapshot.json",
		".dispatcher-state.json",
	)
}

// staleGitignoreEntries returns lines that must NOT appear for the given
// mode. Switching a vault between tracked and untracked modes must remove the
// other mode's entries, otherwise tracked mode silently keeps ignoring the
// backlog.
func staleGitignoreEntries(trackIssues bool) []string {
	if trackIssues {
		return []string{
			".nd.yaml",
			"issues/",
			"# Default mode keeps .nd.yaml and issues/ local; use `nd init --track-issues` to track them in git",
			"# Default mode keeps .nd.yaml and issues/ out of code branches; `nd sync` persists them on the backlog branch",
		}
	}
	return []string{
		"# Tracked mode keeps .nd.yaml and issues/ in git",
		// Pre-sync wording, replaced by the sync-aware comment line.
		"# Default mode keeps .nd.yaml and issues/ local; use `nd init --track-issues` to track them in git",
	}
}

// EnsureGitignore reconciles the vault's .gitignore with the current mode:
// missing entries are added and entries belonging to the opposite
// tracked/untracked mode are removed. Safe to call on every Open.
func (s *Store) EnsureGitignore() error {
	return ensureGitignore(s.dir, s.config.TrackIssues)
}

func ensureGitignore(dir string, trackIssues bool) error {
	path := filepath.Join(dir, ".gitignore")

	var lines []string
	existing := make(map[string]bool)
	f, err := os.Open(path)
	if err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			existing[scanner.Text()] = true
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan .gitignore: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("open .gitignore: %w", err)
	}

	stale := make(map[string]bool)
	for _, entry := range staleGitignoreEntries(trackIssues) {
		stale[entry] = true
	}

	removed := false
	kept := lines[:0:0]
	for _, line := range lines {
		if stale[line] {
			removed = true
			continue
		}
		kept = append(kept, line)
	}

	var missing []string
	for _, entry := range gitignoreEntries(trackIssues) {
		if !existing[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 && !removed {
		return nil // nothing to do
	}

	if len(kept) > 0 && len(missing) > 0 {
		kept = append(kept, "")
	}
	kept = append(kept, missing...)

	content := strings.Join(kept, "\n") + "\n"
	tmp, err := os.CreateTemp(dir, ".gitignore.tmp-*")
	if err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Config holds vault-level nd configuration stored in .nd.yaml.
type Config struct {
	Version         string `yaml:"version"`
	Prefix          string `yaml:"prefix"`
	CreatedBy       string `yaml:"created_by"`
	TrackIssues     bool   `yaml:"track_issues,omitempty"`
	StatusCustom    string `yaml:"status_custom,omitempty"`
	StatusSequence  string `yaml:"status_sequence,omitempty"`
	StatusFSM       bool   `yaml:"status_fsm,omitempty"`
	StatusExitRules string `yaml:"status_exit_rules,omitempty"`
	SyncBranch      string `yaml:"sync_branch,omitempty"`
	SyncRemote      string `yaml:"sync_remote,omitempty"`
	SyncAuto        string `yaml:"sync_auto,omitempty"` // "", "on" = auto-snapshot after mutations; "off" disables
}

type InitOptions struct {
	TrackIssues bool
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
		return nil, lockError(dir, err)
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

// OpenRead opens an existing nd vault with a shared advisory lock. Multiple
// readers proceed concurrently; only writers (Open) exclude them. vlt's
// atomic temp-file + rename writes guarantee a reader never sees a torn file.
// Callers must not mutate the vault through a read-mode store.
func OpenRead(dir string) (*Store, error) {
	unlock, err := vlt.LockVault(dir, false)
	if err != nil {
		return nil, lockError(dir, err)
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
	return s, nil
}

// lockError wraps a lock acquisition failure with actionable context: lock
// contention is expected under multi-agent parallelism and callers should
// retry rather than conclude the vault is broken.
func lockError(dir string, err error) error {
	return fmt.Errorf("vault %s is busy (another nd/vlt process holds the lock; retry, or raise VLT_LOCK_TIMEOUT): %w", dir, err)
}

// Close releases the advisory file lock. Safe to call multiple times.
func (s *Store) Close() {
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
}

// Init creates a new nd vault at dir.
func Init(dir, prefix, author string, opts ...InitOptions) (*Store, error) {
	var initOpts InitOptions
	if len(opts) > 0 {
		initOpts = opts[0]
	}

	// Create vault directory structure.
	for _, sub := range []string{"issues", ".trash"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}

	// Write config.
	cfg := Config{
		Version:     "1",
		Prefix:      prefix,
		CreatedBy:   author,
		TrackIssues: initOpts.TrackIssues,
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".nd.yaml"), data, 0o644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Write .gitignore (idempotent -- appends to any existing file).
	if err := ensureGitignore(dir, cfg.TrackIssues); err != nil {
		return nil, fmt.Errorf("write .gitignore: %w", err)
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

// SyncBranch returns the configured backlog branch, defaulting to nd/backlog.
func (s *Store) SyncBranch() string {
	if s.config.SyncBranch != "" {
		return s.config.SyncBranch
	}
	return "nd/backlog"
}

// SyncRemote returns the configured sync remote, defaulting to origin.
func (s *Store) SyncRemote() string {
	if s.config.SyncRemote != "" {
		return s.config.SyncRemote
	}
	return "origin"
}

// SyncAutoEnabled reports whether mutations should auto-snapshot to the
// backlog branch. Enabled by default; disable with `nd config set sync.auto
// off` or the ND_SYNC_AUTO=off environment variable (checked by the CLI).
func (s *Store) SyncAutoEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(s.config.SyncAuto)) {
	case "off", "false", "0", "no":
		return false
	}
	return true
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

	case "sync.branch":
		if strings.ContainsAny(value, " \t~^:?*[\\") || strings.HasPrefix(value, "-") {
			return fmt.Errorf("invalid branch name %q", value)
		}
		s.config.SyncBranch = value

	case "sync.remote":
		if strings.ContainsAny(value, " \t") {
			return fmt.Errorf("invalid remote name %q", value)
		}
		s.config.SyncRemote = value

	case "sync.auto":
		switch strings.ToLower(value) {
		case "on", "true", "1", "yes", "":
			s.config.SyncAuto = "on"
		case "off", "false", "0", "no":
			s.config.SyncAuto = "off"
		default:
			return fmt.Errorf("invalid value %q for sync.auto: use on or off", value)
		}

	case "track_issues":
		switch strings.ToLower(value) {
		case "true", "1", "yes", "on":
			s.config.TrackIssues = true
		case "false", "0", "no", "off":
			s.config.TrackIssues = false
		default:
			return fmt.Errorf("invalid boolean value %q for track_issues", value)
		}
		if err := s.SaveConfig(); err != nil {
			return err
		}
		// Reconcile .gitignore immediately so the mode switch takes effect.
		return s.EnsureGitignore()

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
	case "sync.branch":
		return s.SyncBranch(), nil
	case "sync.remote":
		return s.SyncRemote(), nil
	case "sync.auto":
		if s.SyncAutoEnabled() {
			return "on", nil
		}
		return "off", nil
	case "track_issues":
		if s.config.TrackIssues {
			return "true", nil
		}
		return "false", nil
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
	syncAuto := "on"
	if !s.SyncAutoEnabled() {
		syncAuto = "off"
	}
	track := "false"
	if s.config.TrackIssues {
		track = "true"
	}
	return [][2]string{
		{"version", s.config.Version},
		{"prefix", s.config.Prefix},
		{"created_by", s.config.CreatedBy},
		{"track_issues", track},
		{"status.custom", s.config.StatusCustom},
		{"status.sequence", s.config.StatusSequence},
		{"status.fsm", fsm},
		{"status.exit_rules", s.config.StatusExitRules},
		{"sync.branch", s.SyncBranch()},
		{"sync.remote", s.SyncRemote()},
		{"sync.auto", syncAuto},
	}
}
