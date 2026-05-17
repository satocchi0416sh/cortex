package claudelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocateJSONL_RootEmpty(t *testing.T) {
	_, err := LocateJSONL("", "abc")
	if err == nil || !strings.Contains(err.Error(), "claude projects root") {
		t.Fatalf("expected root-empty error, got %v", err)
	}
}

func TestLocateJSONL_RootMissing(t *testing.T) {
	_, err := LocateJSONL("/no/such/path/cortex-test-xyz", "abc")
	if err == nil {
		t.Fatalf("expected error for missing root")
	}
}

func TestLocateJSONL_SingleMatch(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "proj-a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "sess-1.jsonl")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LocateJSONL(root, "sess-1")
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestLocateJSONL_NoMatch(t *testing.T) {
	root := t.TempDir()
	_, err := LocateJSONL(root, "ghost")
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected ghost in error, got %v", err)
	}
}

func TestLocateJSONL_MultipleMatchesPicksNewest(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	pa := filepath.Join(a, "dup.jsonl")
	pb := filepath.Join(b, "dup.jsonl")
	if err := os.WriteFile(pa, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(pa, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pb, newer, newer); err != nil {
		t.Fatal(err)
	}
	got, err := LocateJSONL(root, "dup")
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if got != pb {
		t.Errorf("got %q, want newer %q", got, pb)
	}
}
