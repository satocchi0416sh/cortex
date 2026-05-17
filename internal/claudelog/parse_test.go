package claudelog

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newCaptureLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	return slog.New(h), buf
}

func TestParseSession_Sample(t *testing.T) {
	logger, buf := newCaptureLogger()
	s, err := ParseSession("testdata/sample.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.SessionID != "sess-sample" {
		t.Errorf("SessionID = %q", s.SessionID)
	}
	if s.Cwd != "/home/me/proj" {
		t.Errorf("Cwd = %q", s.Cwd)
	}
	if s.StartedAt.IsZero() {
		t.Errorf("StartedAt should be set")
	}
	if len(s.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(s.Messages))
	}
	if s.Messages[0].Role != "user" || s.Messages[0].Text != "hello there" {
		t.Errorf("msg[0] = %+v", s.Messages[0])
	}
	if s.Messages[1].Role != "assistant" || s.Messages[1].Text != "hi back" {
		t.Errorf("msg[1] = %+v", s.Messages[1])
	}
	if s.Messages[0].UUID != "uuid-user-1" {
		t.Errorf("msg[0].UUID = %q, want uuid-user-1", s.Messages[0].UUID)
	}
	if s.Messages[1].UUID != "uuid-asst-1" {
		t.Errorf("msg[1].UUID = %q, want uuid-asst-1", s.Messages[1].UUID)
	}
	if strings.Contains(buf.String(), "unknown entry type") {
		t.Errorf("system is a known skip type, should not warn: %s", buf.String())
	}
}

func TestParseSession_Extended(t *testing.T) {
	logger, _ := newCaptureLogger()
	s, err := ParseSession("testdata/sample_extended.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Messages) != 5 {
		t.Fatalf("Messages len = %d, want 5", len(s.Messages))
	}
	wantUUIDs := []string{"uuid-1", "uuid-2", "uuid-3", "uuid-4", "uuid-5"}
	for i, want := range wantUUIDs {
		if s.Messages[i].UUID != want {
			t.Errorf("msg[%d].UUID = %q, want %q", i, s.Messages[i].UUID, want)
		}
	}
}

func TestParseSession_ArrayContent(t *testing.T) {
	logger, _ := newCaptureLogger()
	s, err := ParseSession("testdata/array_content.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(s.Messages))
	}
	got := s.Messages[1].Text
	want := "hello\nworld"
	if got != want {
		t.Errorf("assistant text = %q, want %q (tool_use must drop, text parts must join with \\n)", got, want)
	}
}

func TestParseSession_UnknownTypeWarns(t *testing.T) {
	logger, buf := newCaptureLogger()
	s, err := ParseSession("testdata/unknown_type.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Errorf("expected unknown type skipped, user+assistant kept; got %d msgs", len(s.Messages))
	}
	if !strings.Contains(buf.String(), "skipping unknown entry type") {
		t.Errorf("expected unknown-type warning, log = %s", buf.String())
	}
	if !strings.Contains(buf.String(), "mystery-type") {
		t.Errorf("warning should mention type name, log = %s", buf.String())
	}
}

func TestParseSession_SidechainSkippedSilently(t *testing.T) {
	logger, buf := newCaptureLogger()
	s, err := ParseSession("testdata/sidechain.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("expected sidechain dropped, got %d msgs: %+v", len(s.Messages), s.Messages)
	}
	for _, m := range s.Messages {
		if strings.Contains(m.Text, "sidechain") {
			t.Errorf("sidechain message leaked: %+v", m)
		}
	}
	if buf.Len() != 0 {
		t.Errorf("sidechain should not log warnings, got: %s", buf.String())
	}
}

// TestParseSession_SidechainOnly is the AC3 regression: a JSONL where every
// non-system entry is a sidechain must produce zero Messages so the Notion
// page never receives a leaked sidechain turn.
func TestParseSession_SidechainOnly(t *testing.T) {
	logger, _ := newCaptureLogger()
	s, err := ParseSession("testdata/sidechain_only.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Fatalf("expected zero messages from sidechain-only file, got %d: %+v", len(s.Messages), s.Messages)
	}
}

func TestParseSession_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logger, _ := newCaptureLogger()
	_, err := ParseSession(path, logger)
	if err == nil {
		t.Fatalf("expected error on empty file")
	}
	if !strings.Contains(err.Error(), "session id") {
		t.Errorf("error should mention missing session id, got: %v", err)
	}
}

func TestParseSession_InvalidJSONSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.jsonl")
	contents := `{"type":"user","sessionId":"sess-x","message":{"role":"user","content":"a"}}
not-json-at-all
{"type":"assistant","sessionId":"sess-x","message":{"role":"assistant","content":"b"}}
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, buf := newCaptureLogger()
	s, err := ParseSession(path, logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Errorf("expected 2 valid messages around the broken line, got %d", len(s.Messages))
	}
	if !strings.Contains(buf.String(), "invalid json entry") {
		t.Errorf("expected invalid-json warning, log = %s", buf.String())
	}
}

// TestParseSession_ToolUseParsedAndPaired exercises the AC1/AC2 path: an
// assistant message with two tool_use parts must surface as ToolUses on the
// assistant Message, and the subsequent user tool_result parts must be
// merged in by ID so the renderer can emit a toggle containing both input
// and result.
func TestParseSession_ToolUseParsedAndPaired(t *testing.T) {
	logger, _ := newCaptureLogger()
	s, err := ParseSession("testdata/tool_use.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.AITitle != "List project files via Bash + Read" {
		t.Errorf("AITitle = %q, want captured top-level aiTitle", s.AITitle)
	}
	// Expect 3 messages: user, assistant(with text+tool_uses), assistant("done").
	// The intermediate user-with-tool_result entry must be dropped because it
	// carries no text and its payload has already been merged into the
	// previous assistant message's ToolUses.
	if len(s.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3 (user, assistant+tools, assistant done): %+v", len(s.Messages), s.Messages)
	}
	asst := s.Messages[1]
	if asst.Role != "assistant" {
		t.Fatalf("msg[1] role = %q, want assistant", asst.Role)
	}
	if asst.Text != "sure, looking now" {
		t.Errorf("asst.Text = %q, want %q", asst.Text, "sure, looking now")
	}
	if len(asst.ToolUses) != 2 {
		t.Fatalf("asst.ToolUses len = %d, want 2", len(asst.ToolUses))
	}
	if asst.ToolUses[0].Name != "Bash" || asst.ToolUses[0].ID != "toolu_1" {
		t.Errorf("tool[0] = %+v", asst.ToolUses[0])
	}
	if !strings.Contains(asst.ToolUses[0].InputJSON, `"command":"ls -la"`) {
		t.Errorf("tool[0].InputJSON = %q, want command", asst.ToolUses[0].InputJSON)
	}
	if asst.ToolUses[0].Result != "total 0\ndrwxr-xr-x" {
		t.Errorf("tool[0].Result = %q, want pair to be merged", asst.ToolUses[0].Result)
	}
	if asst.ToolUses[1].Name != "Read" || asst.ToolUses[1].Result != "file contents here" {
		t.Errorf("tool[1] = %+v", asst.ToolUses[1])
	}
}

// TestParseSession_ArrayContent_NoToolUseAndDropsBareToolUse confirms the
// pre-existing array_content fixture still yields the same joined text (no
// new ToolUses since the fixture's tool_use has no id/input).
func TestParseSession_ArrayContent_ToolUsePresent(t *testing.T) {
	logger, _ := newCaptureLogger()
	s, err := ParseSession("testdata/array_content.jsonl", logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(s.Messages))
	}
	if got := len(s.Messages[1].ToolUses); got != 1 {
		t.Errorf("ToolUses len = %d, want 1 (the tool_use part with no input)", got)
	}
}

func TestParseSession_StartedAtFallbackToModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-ts.jsonl")
	contents := `{"type":"user","sessionId":"sess-noTS","cwd":"/c","message":{"role":"user","content":"a"}}
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, _ := newCaptureLogger()
	s, err := ParseSession(path, logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.StartedAt.IsZero() {
		t.Errorf("StartedAt should fall back to mtime when no timestamp in JSONL")
	}
}
