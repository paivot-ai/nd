package store

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchive_TarGz(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a few issues.
	a, err := s.CreateIssue("Issue A", "description A", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := s.CreateIssue("Issue B", "description B", "bug", 1, "alice", nil, "")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	output := filepath.Join(dir, "test-archive.tar.gz")
	opts := ArchiveOptions{
		Output: output,
		Format: "tar.gz",
	}

	path, err := s.Archive(opts, "test-v1")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if path != output {
		t.Errorf("path = %q, want %q", path, output)
	}

	// Open and inspect the archive.
	f, err := os.Open(output)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	found := make(map[string]bool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		found[hdr.Name] = true
	}

	// Verify expected files.
	expected := []string{
		"issues/" + a.ID + ".md",
		"issues/" + b.ID + ".md",
		".nd.yaml",
		"manifest.json",
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("archive missing %q", name)
		}
	}
}

func TestArchive_ClosedOnly(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create two issues, close one.
	open, err := s.CreateIssue("Open issue", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create open: %v", err)
	}
	closed, err := s.CreateIssue("Closed issue", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create closed: %v", err)
	}
	if err := s.CloseIssue(closed.ID, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}

	output := filepath.Join(dir, "closed-only.tar.gz")
	opts := ArchiveOptions{
		Output:     output,
		ClosedOnly: true,
		Format:     "tar.gz",
	}

	if _, err := s.Archive(opts, "test-v1"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Inspect archive.
	files := tarFileNames(t, output)
	closedFile := "issues/" + closed.ID + ".md"
	openFile := "issues/" + open.ID + ".md"

	if !files[closedFile] {
		t.Errorf("archive should contain closed issue %q", closedFile)
	}
	if files[openFile] {
		t.Errorf("archive should NOT contain open issue %q", openFile)
	}
}

func TestArchive_Manifest(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := filepath.Join(dir, "manifest-test.tar.gz")
	opts := ArchiveOptions{
		Output: output,
		Format: "tar.gz",
	}

	if _, err := s.Archive(opts, "v0.42.0"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Extract manifest.json from the archive.
	manifest := extractFileFromTar(t, output, "manifest.json")
	var m ArchiveManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if m.IssueCount != 1 {
		t.Errorf("manifest issue_count = %d, want 1", m.IssueCount)
	}
	if m.Prefix != "TST" {
		t.Errorf("manifest prefix = %q, want TST", m.Prefix)
	}
	if m.Version != "v0.42.0" {
		t.Errorf("manifest version = %q, want v0.42.0", m.Version)
	}
	if m.Timestamp == "" {
		t.Error("manifest timestamp should not be empty")
	}
	if m.Filter != "all" {
		t.Errorf("manifest filter = %q, want all", m.Filter)
	}
}

func TestArchive_RemoveArchived(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	open, err := s.CreateIssue("Open issue", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create open: %v", err)
	}
	closed, err := s.CreateIssue("Closed issue", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create closed: %v", err)
	}
	if err := s.CloseIssue(closed.ID, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}

	output := filepath.Join(dir, "remove-test.tar.gz")
	opts := ArchiveOptions{
		Output:         output,
		Format:         "tar.gz",
		RemoveArchived: true,
	}

	if _, err := s.Archive(opts, "test-v1"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Closed issue should be moved to .trash/.
	closedPath := filepath.Join(dir, "issues", closed.ID+".md")
	if _, err := os.Stat(closedPath); !os.IsNotExist(err) {
		t.Error("closed issue should be removed from issues/")
	}
	trashPath := filepath.Join(dir, ".trash", closed.ID+".md")
	if _, err := os.Stat(trashPath); err != nil {
		t.Errorf("closed issue should be in .trash/: %v", err)
	}

	// Open issue should still be in issues/.
	openPath := filepath.Join(dir, "issues", open.ID+".md")
	if _, err := os.Stat(openPath); err != nil {
		t.Errorf("open issue should remain in issues/: %v", err)
	}
}

func TestArchive_JSONL(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "TST", "tester")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = s.CreateIssue("Issue A", "", "task", 2, "", nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := filepath.Join(dir, "archive.jsonl")
	opts := ArchiveOptions{
		Output: output,
		Format: "jsonl",
	}

	if _, err := s.Archive(opts, "test-v1"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (manifest + 1 issue), got %d", len(lines))
	}

	// First line should be the manifest.
	var m ArchiveManifest
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatalf("unmarshal manifest line: %v", err)
	}
	if m.IssueCount != 1 {
		t.Errorf("manifest issue_count = %d, want 1", m.IssueCount)
	}
}

// --- helpers ---

func tarFileNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tar: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	names := make(map[string]bool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		names[hdr.Name] = true
	}
	return names
}

func extractFileFromTar(t *testing.T, archivePath, fileName string) []byte {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open tar: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Name == fileName {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read %s from tar: %v", fileName, err)
			}
			return data
		}
	}
	t.Fatalf("file %q not found in archive", fileName)
	return nil
}
