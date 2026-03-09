package store

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RamXX/nd/internal/model"
)

// ArchiveOptions controls what the archive includes and where it goes.
type ArchiveOptions struct {
	Output         string // output file path (empty = default)
	ClosedOnly     bool   // only include closed issues
	Since          string // only include issues updated after this date (RFC3339 or YYYY-MM-DD)
	Format         string // "tar.gz" or "jsonl"
	RemoveArchived bool   // move archived closed issues to .trash/
}

// ArchiveManifest is written as manifest.json inside the archive.
type ArchiveManifest struct {
	Timestamp  string `json:"timestamp"`
	IssueCount int    `json:"issue_count"`
	Prefix     string `json:"prefix"`
	Version    string `json:"nd_version"`
	Filter     string `json:"filter"`
}

// Archive creates a compressed, git-committable snapshot of the backlog.
func (s *Store) Archive(opts ArchiveOptions, ndVersion string) (string, error) {
	format := opts.Format
	if format == "" {
		format = "tar.gz"
	}
	if format != "tar.gz" && format != "jsonl" {
		return "", fmt.Errorf("unsupported format %q: must be tar.gz or jsonl", format)
	}

	// Determine output path.
	output := opts.Output
	if output == "" {
		dateStr := time.Now().UTC().Format("2006-01-02")
		ext := format
		if format == "jsonl" {
			ext = "jsonl"
		}
		output = filepath.Join(s.dir, fmt.Sprintf("archive-%s.%s", dateStr, ext))
	}

	// Parse --since filter.
	var sinceTime time.Time
	if opts.Since != "" {
		var err error
		sinceTime, err = parseSinceDate(opts.Since)
		if err != nil {
			return "", fmt.Errorf("invalid --since date %q: %w", opts.Since, err)
		}
	}

	// Collect matching issues.
	fopts := FilterOptions{Status: "all"}
	allIssues, err := s.ListIssues(fopts)
	if err != nil {
		return "", fmt.Errorf("list issues: %w", err)
	}

	var issues []*model.Issue
	for _, issue := range allIssues {
		if opts.ClosedOnly && issue.Status != model.StatusClosed {
			continue
		}
		if !sinceTime.IsZero() && !issue.UpdatedAt.After(sinceTime) {
			continue
		}
		issues = append(issues, issue)
	}

	// Build filter description for manifest.
	filterDesc := buildFilterDescription(opts)

	manifest := ArchiveManifest{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		IssueCount: len(issues),
		Prefix:     s.config.Prefix,
		Version:    ndVersion,
		Filter:     filterDesc,
	}

	switch format {
	case "tar.gz":
		if err := s.writeTarGzArchive(output, issues, manifest); err != nil {
			return "", err
		}
	case "jsonl":
		if err := s.writeJSONLArchive(output, issues, manifest); err != nil {
			return "", err
		}
	}

	// Optionally remove archived closed issues.
	if opts.RemoveArchived {
		for _, issue := range issues {
			if issue.Status != model.StatusClosed {
				continue
			}
			src := filepath.Join(s.dir, "issues", issue.ID+".md")
			dst := filepath.Join(s.dir, ".trash", issue.ID+".md")
			if err := os.Rename(src, dst); err != nil {
				// Best-effort: log but continue.
				continue
			}
		}
	}

	return output, nil
}

func (s *Store) writeTarGzArchive(output string, issues []*model.Issue, manifest ArchiveManifest) error {
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Add issue files.
	for _, issue := range issues {
		path := filepath.Join(s.dir, "issues", issue.ID+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read issue %s: %w", issue.ID, err)
		}
		hdr := &tar.Header{
			Name:    "issues/" + issue.ID + ".md",
			Size:    int64(len(data)),
			Mode:    0o644,
			ModTime: issue.UpdatedAt,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header for %s: %w", issue.ID, err)
		}
		if _, err := tw.Write(data); err != nil {
			return fmt.Errorf("write tar data for %s: %w", issue.ID, err)
		}
	}

	// Add .nd.yaml config.
	cfgPath := filepath.Join(s.dir, ".nd.yaml")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read .nd.yaml: %w", err)
	}
	cfgHdr := &tar.Header{
		Name:    ".nd.yaml",
		Size:    int64(len(cfgData)),
		Mode:    0o644,
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(cfgHdr); err != nil {
		return fmt.Errorf("write tar header for .nd.yaml: %w", err)
	}
	if _, err := tw.Write(cfgData); err != nil {
		return fmt.Errorf("write tar data for .nd.yaml: %w", err)
	}

	// Add manifest.json.
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	manifestHdr := &tar.Header{
		Name:    "manifest.json",
		Size:    int64(len(manifestData)),
		Mode:    0o644,
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(manifestHdr); err != nil {
		return fmt.Errorf("write tar header for manifest.json: %w", err)
	}
	if _, err := tw.Write(manifestData); err != nil {
		return fmt.Errorf("write tar data for manifest.json: %w", err)
	}

	return nil
}

func (s *Store) writeJSONLArchive(output string, issues []*model.Issue, manifest ArchiveManifest) error {
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create jsonl archive: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	// First line: manifest.
	if err := enc.Encode(manifest); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	// Subsequent lines: one issue per line (frontmatter fields as JSON).
	for _, issue := range issues {
		if err := enc.Encode(issue); err != nil {
			return fmt.Errorf("encode issue %s: %w", issue.ID, err)
		}
	}

	return nil
}

func parseSinceDate(s string) (time.Time, error) {
	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try YYYY-MM-DD.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD format")
}

func buildFilterDescription(opts ArchiveOptions) string {
	var parts []string
	if opts.ClosedOnly {
		parts = append(parts, "closed-only")
	}
	if opts.Since != "" {
		parts = append(parts, "since:"+opts.Since)
	}
	if len(parts) == 0 {
		return "all"
	}
	return strings.Join(parts, ",")
}
