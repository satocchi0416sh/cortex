package claudelog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Message is a single conversational turn the renderer cares about. Role is
// either "user" or "assistant"; other entry types are filtered out at parse
// time. UUID is Claude Code's per-entry identifier, used by the incremental
// sync path to locate the last synced message; older JSONL files without uuid
// fields parse to an empty string and the append path treats that as "cursor
// unknown" which forces a full-sync error rather than silent divergence.
//
// ToolUses is the ordered list of tool invocations Claude Code emitted as
// part of this message's content array. Always nil for user messages
// because user tool_result parts are merged into the matching assistant
// message's ToolUses[i].Result rather than surfacing as standalone entries.
type Message struct {
	Role     string
	Text     string
	UUID     string
	ToolUses []ToolUse
}

// Session is the parsed JSONL: identifying metadata plus the ordered list of
// user/assistant text turns. AITitle carries the value Claude Code wrote in
// the session's ai-title entry (empty when no such entry exists, in which
// case Title falls back to the first user prompt or a synthetic label).
type Session struct {
	SessionID string
	Cwd       string
	StartedAt time.Time
	AITitle   string
	Messages  []Message
}

// knownSkipTypes are entry types we silently skip — they exist in the JSONL
// stream but are not part of the user/assistant transcript the renderer
// emits. Anything outside this set and {"user","assistant","ai-title"} is
// treated as unknown and warn-logged so we notice when Claude Code adds a
// new type.
//
// "ai-title" is intentionally NOT in this set anymore: it carries the
// session title in a top-level aiTitle field that ParseSession captures
// into Session.AITitle.
//
// "tool_use" / "tool_result" are kept here because Claude Code embeds them
// inside user/assistant message.content arrays, not as standalone top-level
// entries; on the rare day a top-level one shows up there is nothing to
// pair it against so silent skip is the correct behaviour.
var knownSkipTypes = map[string]struct{}{
	"system":                {},
	"attachment":            {},
	"file-history-snapshot": {},
	"last-prompt":           {},
	"permission-mode":       {},
	"summary":               {},
	"result":                {},
	"tool_use":              {},
	"tool_result":           {},
}

// ParseSession reads a Claude Code JSONL session file from disk and returns
// the extracted Session. Invalid JSON lines and unknown entry types are
// warn-logged and skipped so partial corruption does not abort the sync.
// Returns an error when the session id cannot be discovered (e.g. empty file).
func ParseSession(path string, logger *slog.Logger) (*Session, error) {
	if logger == nil {
		logger = slog.Default()
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open jsonl: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// 16 MiB max line: Claude Code can write tool_result entries (file dumps,
	// command output) that blow past bufio's 64 KiB default. 64 KiB initial
	// keeps the steady-state allocation tiny for normal lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	session := &Session{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var entry RawEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			logger.Warn("invalid json entry", "line", lineNo, "err", err)
			continue
		}
		// Capture identifying metadata before the sidechain bailout: a
		// sidechain entry still belongs to the session, so its sessionId /
		// cwd / timestamp are valid sources of truth. Without this a
		// JSONL whose first entries are all sidechain (rare but possible)
		// would fail the "session id not found" guard at the end.
		if session.SessionID == "" && entry.SessionID != "" {
			session.SessionID = entry.SessionID
		}
		if session.Cwd == "" && entry.Cwd != "" {
			session.Cwd = entry.Cwd
		}
		if session.StartedAt.IsZero() && entry.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
				session.StartedAt = ts
			} else if ts, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
				session.StartedAt = ts
			}
		}
		if entry.IsSidechain {
			continue
		}

		switch entry.Type {
		case "ai-title":
			// Prefer the first ai-title we see; Claude Code occasionally
			// regenerates titles mid-session, but the original one is more
			// faithful to the conversation kickoff.
			if session.AITitle == "" && entry.AITitle != "" {
				session.AITitle = entry.AITitle
			}
		case "user", "assistant":
			parts := extractParts(entry.Message)
			text, toolUses, results := splitContentParts(parts)
			// Drop empty messages (no text and no tool activity) the same way
			// MVP did. Pure tool_result-only user entries are also dropped
			// here because their payload has already been merged into the
			// matching assistant Message.ToolUses by mergeToolResults below.
			if text == "" && len(toolUses) == 0 && len(results) == 0 {
				continue
			}
			if len(results) > 0 {
				mergeToolResults(session.Messages, results)
			}
			if text == "" && len(toolUses) == 0 {
				continue
			}
			session.Messages = append(session.Messages, Message{
				Role:     entry.Type,
				Text:     text,
				UUID:     entry.UUID,
				ToolUses: toolUses,
			})
		default:
			if _, ok := knownSkipTypes[entry.Type]; ok {
				continue
			}
			logger.Warn("skipping unknown entry type", "type", entry.Type, "line", lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan jsonl: %w", err)
	}

	if session.SessionID == "" {
		return nil, errors.New("session id not found in jsonl")
	}
	if session.StartedAt.IsZero() {
		if info, err := os.Stat(path); err == nil {
			session.StartedAt = info.ModTime().UTC()
		}
	}
	return session, nil
}

// splitContentParts walks a decoded content-part slice and returns three
// slices grouped by what the renderer needs: the joined text (text parts
// concatenated with newlines, matching the MVP extractText shape), the
// ordered tool_use invocations, and the tool_result back-references that
// the caller will merge into prior assistant messages.
func splitContentParts(parts []contentPart) (string, []ToolUse, []contentPart) {
	var textPieces []string
	var toolUses []ToolUse
	var results []contentPart
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				textPieces = append(textPieces, p.Text)
			}
		case "tool_use":
			toolUses = append(toolUses, ToolUse{
				ID:        p.ID,
				Name:      p.Name,
				InputJSON: p.InputJSON,
			})
		case "tool_result":
			results = append(results, p)
		}
	}
	return strings.Join(textPieces, "\n"), toolUses, results
}

// mergeToolResults walks back over already-parsed messages to fill the
// Result field of any ToolUse whose ID matches an incoming tool_result
// part. We scan in reverse because the matching tool_use is almost always
// in the immediately preceding assistant message, so the inner loop usually
// exits after one iteration; orphaned results (no matching tool_use, e.g.
// JSONL truncated mid-pair) are silently dropped.
func mergeToolResults(messages []Message, results []contentPart) {
	for _, r := range results {
		if r.ToolUseID == "" {
			continue
		}
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role != "assistant" {
				continue
			}
			matched := false
			for j := range messages[i].ToolUses {
				if messages[i].ToolUses[j].ID == r.ToolUseID {
					messages[i].ToolUses[j].Result = r.Result
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
	}
}
