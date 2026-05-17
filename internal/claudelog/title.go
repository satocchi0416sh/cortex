package claudelog

import (
	"path/filepath"
	"strings"
)

const (
	// maxFirstPromptLen caps how many runes of the first user message land
	// in the Notion Name field. 50 keeps DB list rows readable without
	// dropping sentence-boundary cues from short prompts.
	maxFirstPromptLen = 50
	// sessionIDPrefixLen is how many characters of the UUID get used in
	// the synthetic fallback title; 8 matches MVP convention and stays
	// unique enough for human scanning within one session day.
	sessionIDPrefixLen = 8
)

// Title computes a non-empty Notion "Name" title for a Session via three
// fallback layers. Used by claude-log to keep the Notion DB list scannable
// even when Claude Code did not assign an ai-title (e.g. very short
// sessions).
//
// Order:
//  1. session.AITitle (Claude's own one-line summary, when present)
//  2. First user message text trimmed to maxFirstPromptLen runes
//  3. Synthetic "<sessionID[:8]> <cwd basename>" derived from metadata
//
// At least one layer always returns a non-empty string because the
// synthetic fallback degrades gracefully to "claude-log" when both
// sessionID and cwd are empty. A nil session also yields "claude-log" so
// callers do not need to guard.
func Title(s *Session) string {
	if s == nil {
		return "claude-log"
	}
	if t := strings.TrimSpace(s.AITitle); t != "" {
		return t
	}
	for _, m := range s.Messages {
		if m.Role != "user" {
			continue
		}
		if t := strings.TrimSpace(m.Text); t != "" {
			return truncateTitle(t, maxFirstPromptLen)
		}
	}
	sid := s.SessionID
	if len(sid) > sessionIDPrefixLen {
		sid = sid[:sessionIDPrefixLen]
	}
	base := filepath.Base(s.Cwd)
	if s.Cwd == "" || base == "." || base == "/" {
		base = "claude-log"
	}
	title := strings.TrimSpace(sid + " " + base)
	if title == "" {
		return "claude-log"
	}
	return title
}

// truncateTitle clips s to max runes, appending an ellipsis when truncation
// happens so readers can tell the title was cut.
func truncateTitle(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + "…"
}
