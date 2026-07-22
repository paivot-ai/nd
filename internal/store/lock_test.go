package store

import "testing"

func TestSharedReadLocksCoexist(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir, "T", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateIssue("A", "d", "task", 2, "", nil, ""); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VLT_LOCK_TIMEOUT", "300ms")

	r1, err := OpenRead(dir)
	if err != nil {
		t.Fatalf("first reader: %v", err)
	}
	defer r1.Close()

	// A second concurrent reader must not block.
	r2, err := OpenRead(dir)
	if err != nil {
		t.Fatalf("second concurrent reader: %v", err)
	}
	defer r2.Close()

	// A writer must be excluded while readers hold the shared lock.
	if _, err := Open(dir); err == nil {
		t.Fatal("expected exclusive open to fail while readers hold the lock")
	}

	r1.Close()
	r2.Close()

	w, err := Open(dir)
	if err != nil {
		t.Fatalf("writer after readers closed: %v", err)
	}
	w.Close()
}
