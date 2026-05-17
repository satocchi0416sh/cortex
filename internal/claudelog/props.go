package claudelog

import (
	"time"

	"github.com/satocchi0416sh/cortex/internal/notion"
)

// BuildProperties materialises a Session into the Notion property struct
// the claude-log DB schema expects. The title is computed by Title which
// layers an ai-title preference, a first-user-prompt fallback, and a
// synthetic "<sid8> <cwd-basename>" last-resort so the Notion Name column
// is never empty. now is injected so tests can pin LastSynced
// deterministically.
func BuildProperties(s *Session, srcPath string, now time.Time) notion.ClaudeLogProperties {
	var lastUUID string
	if n := len(s.Messages); n > 0 {
		lastUUID = s.Messages[n-1].UUID
	}
	return notion.ClaudeLogProperties{
		Title:        Title(s),
		SessionID:    s.SessionID,
		Project:      s.Cwd,
		SourcePath:   srcPath,
		StartedAt:    s.StartedAt,
		LastSynced:   now.UTC(),
		LastUUID:     lastUUID,
		MessageCount: len(s.Messages),
	}
}
