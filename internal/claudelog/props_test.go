package claudelog

import (
	"testing"
	"time"
)

func TestBuildProperties_Standard(t *testing.T) {
	s := &Session{
		SessionID: "abcdef0123-456",
		Cwd:       "/Users/me/Projects/dotgo",
		StartedAt: time.Date(2026, 5, 17, 9, 0, 0, 0, time.UTC),
		Messages: []Message{
			{Role: "user", Text: "a", UUID: "u-1"},
			{Role: "assistant", Text: "b", UUID: "u-2"},
		},
	}
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.FixedZone("JST", 9*3600))
	p := BuildProperties(s, "/path/to/abcdef01.jsonl", now)
	if p.Title != "abcdef01 dotgo" {
		t.Errorf("Title = %q, want %q", p.Title, "abcdef01 dotgo")
	}
	if p.SessionID != s.SessionID {
		t.Errorf("SessionID truncated: got %q", p.SessionID)
	}
	if p.Project != s.Cwd {
		t.Errorf("Project = %q", p.Project)
	}
	if p.SourcePath != "/path/to/abcdef01.jsonl" {
		t.Errorf("SourcePath = %q", p.SourcePath)
	}
	if p.LastSynced.Location() != time.UTC {
		t.Errorf("LastSynced not UTC: %v", p.LastSynced)
	}
	if p.LastUUID != "u-2" {
		t.Errorf("LastUUID = %q, want u-2 (last message's UUID)", p.LastUUID)
	}
	if p.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", p.MessageCount)
	}
}

func TestBuildProperties_EmptyMessages(t *testing.T) {
	s := &Session{SessionID: "sid", Cwd: "/proj/x"}
	p := BuildProperties(s, "src", time.Now())
	if p.LastUUID != "" {
		t.Errorf("LastUUID should be empty for sessions with no messages, got %q", p.LastUUID)
	}
	if p.MessageCount != 0 {
		t.Errorf("MessageCount = %d, want 0", p.MessageCount)
	}
}

func TestBuildProperties_ShortSessionID(t *testing.T) {
	s := &Session{SessionID: "abc12", Cwd: "/x/y/z"}
	p := BuildProperties(s, "src", time.Now())
	if p.Title != "abc12 z" {
		t.Errorf("Title = %q, want %q", p.Title, "abc12 z")
	}
}

func TestBuildProperties_EmptyCwd(t *testing.T) {
	s := &Session{SessionID: "abc12345-rest", Cwd: ""}
	p := BuildProperties(s, "src", time.Now())
	if p.Title != "abc12345 claude-log" {
		t.Errorf("Title = %q", p.Title)
	}
}

func TestBuildProperties_RootCwd(t *testing.T) {
	s := &Session{SessionID: "abc12345-rest", Cwd: "/"}
	p := BuildProperties(s, "src", time.Now())
	if p.Title != "abc12345 claude-log" {
		t.Errorf("Title = %q", p.Title)
	}
}

func TestBuildProperties_EmptySessionID(t *testing.T) {
	s := &Session{SessionID: "", Cwd: "/proj/abc"}
	p := BuildProperties(s, "src", time.Now())
	if p.Title != "abc" {
		t.Errorf("Title = %q (sid empty so title is just basename, fine if non-empty)", p.Title)
	}
}
