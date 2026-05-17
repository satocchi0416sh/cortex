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
	if strings.Contains(buf.String(), "unknown entry type") {
		t.Errorf("system is a known skip type, should not warn: %s", buf.String())
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
