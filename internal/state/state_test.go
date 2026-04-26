package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does", "not", "exist.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Files) != 0 {
		t.Errorf("expected empty Files, got %d", len(s.Files))
	}
	if s.Version != currentVersion {
		t.Errorf("expected version=%d, got %d", currentVersion, s.Version)
	}
}

func TestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s.Set("ext1", FileEntry{
		AbsPath:     "/tmp/a.md",
		Project:     "proj",
		RelPath:     ".serena/memories/a.md",
		Filename:    "a.md",
		ContentHash: "deadbeef",
		PageID:      "page-uuid",
		LastSynced:  now,
	})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load2: %v", err)
	}
	got, ok := loaded.Get("ext1")
	if !ok {
		t.Fatal("entry missing after roundtrip")
	}
	if got.PageID != "page-uuid" || got.ContentHash != "deadbeef" {
		t.Errorf("entry lost data: %+v", got)
	}
	if !got.LastSynced.Equal(now) {
		t.Errorf("time mismatch: got=%v want=%v", got.LastSynced, now)
	}
}

func TestEmptyFileInitializes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Files == nil {
		t.Error("expected non-nil Files map")
	}
}
