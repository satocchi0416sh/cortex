package claudelog

import (
	"strings"
	"testing"
)

func TestTitle_NilSession(t *testing.T) {
	if got := Title(nil); got != "claude-log" {
		t.Errorf("Title(nil) = %q, want %q", got, "claude-log")
	}
}

func TestTitle_AITitlePreferred(t *testing.T) {
	s := &Session{
		AITitle:   "  Refactor sync module  ",
		SessionID: "abcdef01-rest",
		Cwd:       "/p/x",
		Messages: []Message{
			{Role: "user", Text: "hi"},
		},
	}
	got := Title(s)
	want := "Refactor sync module"
	if got != want {
		t.Errorf("Title = %q, want trimmed AITitle %q", got, want)
	}
}

func TestTitle_FirstUserPromptFallback(t *testing.T) {
	s := &Session{
		SessionID: "abcdef01-rest",
		Cwd:       "/p/x",
		Messages: []Message{
			{Role: "assistant", Text: "should be skipped"},
			{Role: "user", Text: "  please refactor parse.go  "},
			{Role: "user", Text: "second user, also skipped"},
		},
	}
	got := Title(s)
	want := "please refactor parse.go"
	if got != want {
		t.Errorf("Title = %q, want %q (first user msg trimmed)", got, want)
	}
}

func TestTitle_FirstUserPromptTruncated(t *testing.T) {
	long := strings.Repeat("x", maxFirstPromptLen+10)
	s := &Session{
		Messages: []Message{{Role: "user", Text: long}},
	}
	got := Title(s)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("Title = %q, want ellipsis suffix on long prompt", got)
	}
	if runeLen := len([]rune(got)); runeLen != maxFirstPromptLen+1 {
		t.Errorf("Title rune len = %d, want %d (cap + ellipsis)", runeLen, maxFirstPromptLen+1)
	}
}

func TestTitle_SyntheticFallback(t *testing.T) {
	s := &Session{
		SessionID: "abcdef0123-rest",
		Cwd:       "/Users/me/Projects/dotgo",
	}
	got := Title(s)
	want := "abcdef01 dotgo"
	if got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
}

func TestTitle_SyntheticFallback_RootCwd(t *testing.T) {
	s := &Session{
		SessionID: "abcdef0123-rest",
		Cwd:       "/",
	}
	got := Title(s)
	want := "abcdef01 claude-log"
	if got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
}

func TestTitle_EmptyEverything(t *testing.T) {
	s := &Session{}
	got := Title(s)
	if got != "claude-log" {
		t.Errorf("Title = %q, want %q (must never be empty)", got, "claude-log")
	}
}

// TestTitle_BlankAITitleFallsThrough guards against treating a whitespace-
// only ai-title as a real title; that would emit an empty Name to Notion
// and violate AC4.
func TestTitle_BlankAITitleFallsThrough(t *testing.T) {
	s := &Session{
		AITitle:   "   ",
		SessionID: "abcdef01-rest",
		Cwd:       "/x",
		Messages:  []Message{{Role: "user", Text: "kept"}},
	}
	got := Title(s)
	if got != "kept" {
		t.Errorf("Title = %q, want first user prompt %q", got, "kept")
	}
}

// TestTitle_FallbackUsesFirstJSONLPrompt ties together AC5: the synthetic
// path is reachable only when no user prompt exists, so when the user has
// spoken at least once that prompt is the layer 2 source of truth.
func TestTitle_FallbackUsesFirstJSONLPrompt(t *testing.T) {
	s := &Session{
		SessionID: "abcdef01-rest",
		Cwd:       "/x",
		Messages: []Message{
			{Role: "user", Text: "hello world"},
			{Role: "assistant", Text: "hi"},
		},
	}
	if got := Title(s); got != "hello world" {
		t.Errorf("Title = %q, want %q (AC5: fallback uses first JSONL prompt)", got, "hello world")
	}
}
