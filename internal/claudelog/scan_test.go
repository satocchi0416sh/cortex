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

func TestLocateAllJSONLs_RootEmpty(t *testing.T) {
	_, err := LocateAllJSONLs("")
	if err == nil || !strings.Contains(err.Error(), "claude projects root") {
		t.Fatalf("expected root-empty error, got %v", err)
	}
}

func TestLocateAllJSONLs_RootMissing(t *testing.T) {
	got, err := LocateAllJSONLs(filepath.Join(t.TempDir(), "no-such-dir"))
	if err != nil {
		t.Fatalf("missing root should warn+return nil, got err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice for missing root, got %v", got)
	}
}

func TestLocateAllJSONLs_Empty(t *testing.T) {
	root := t.TempDir()
	got, err := LocateAllJSONLs(root)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestLocateAllJSONLs_Multiple(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "proj-a")
	deep := filepath.Join(root, "proj-b", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(sub, "sess-a.jsonl")
	b := filepath.Join(deep, "sess-b.jsonl")
	ignore := filepath.Join(sub, "notes.txt")
	for _, p := range []string{a, b, ignore} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LocateAllJSONLs(root)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 jsonl, got %d (%v)", len(got), got)
	}
	// sort.Strings → "proj-a/sess-a.jsonl" < "proj-b/nested/sess-b.jsonl"
	if got[0] != a || got[1] != b {
		t.Errorf("got %v; want [%s %s]", got, a, b)
	}
}

// TestLocateAllJSONLs_SkipsSubagents (Issue #22) guards against scanning
// "<project>/<session>/subagents/agent-*.jsonl" files. Their entries reuse
// the parent session's sessionId, so syncing them collides with the parent
// page and produces cursor-stale errors.
func TestLocateAllJSONLs_SkipsSubagents(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj-a", "session-uuid")
	subagents := filepath.Join(proj, "subagents")
	if err := os.MkdirAll(subagents, 0o755); err != nil {
		t.Fatal(err)
	}
	// Parent session jsonl: included
	parent := filepath.Join(proj, "session-uuid.jsonl")
	// Subagent jsonls: must be excluded
	agent1 := filepath.Join(subagents, "agent-aaaa.jsonl")
	agent2 := filepath.Join(subagents, "agent-bbbb.jsonl")
	for _, p := range []string{parent, agent1, agent2} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := LocateAllJSONLs(root)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only parent jsonl, got %d (%v)", len(got), got)
	}
	if got[0] != parent {
		t.Errorf("got %q, want parent %q", got[0], parent)
	}
	for _, p := range got {
		if strings.Contains(p, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
			t.Errorf("subagent path leaked into result: %q", p)
		}
	}
}

func TestSessionIDFromPath(t *testing.T) {
	cases := map[string]string{
		"/tmp/proj/abc.jsonl":              "abc",
		"/tmp/proj/sub/uuid-1234.jsonl":    "uuid-1234",
		"abc.jsonl":                        "abc",
		"/tmp/proj/abc.JSONL":              "abc",
	}
	for in, want := range cases {
		if got := SessionIDFromPath(in); got != want {
			t.Errorf("SessionIDFromPath(%q) = %q; want %q", in, got, want)
		}
	}
}
