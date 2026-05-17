package claudelog

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/satocchi0416sh/cortex/internal/notion"
)

// BuildProperties materialises a Session into the Notion property struct the
// claude-log DB schema expects. The title is the first 8 chars of the session
// id followed by the project basename, falling back to "claude-log" when the
// session lacks identifying metadata. now is injected so tests can pin
// LastSynced deterministically.
func BuildProperties(s *Session, srcPath string, now time.Time) notion.ClaudeLogProperties {
	sid := s.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	base := filepath.Base(s.Cwd)
	if s.Cwd == "" || base == "." || base == "/" {
		base = "claude-log"
	}
	title := strings.TrimSpace(sid + " " + base)
	if title == "" {
		title = "claude-log"
	}
	var lastUUID string
	if n := len(s.Messages); n > 0 {
		lastUUID = s.Messages[n-1].UUID
	}
	return notion.ClaudeLogProperties{
		Title:        title,
		SessionID:    s.SessionID,
		Project:      s.Cwd,
		SourcePath:   srcPath,
		StartedAt:    s.StartedAt,
		LastSynced:   now.UTC(),
		LastUUID:     lastUUID,
		MessageCount: len(s.Messages),
	}
}
