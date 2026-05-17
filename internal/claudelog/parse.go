package claudelog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// Message is a single conversational turn the renderer cares about. Role is
// either "user" or "assistant"; other entry types are filtered out at parse
// time.
type Message struct {
	Role string
	Text string
}

// Session is the parsed JSONL: identifying metadata plus the ordered list of
// user/assistant text turns.
type Session struct {
	SessionID string
	Cwd       string
	StartedAt time.Time
	Messages  []Message
}

// knownSkipTypes are entry types we silently skip — they exist in the JSONL
// stream but are not part of the user/assistant transcript MVP renders.
// Anything outside both this set and {"user","assistant"} is treated as
// unknown and warn-logged so we notice when Claude Code adds a new type.
var knownSkipTypes = map[string]struct{}{
	"system":                {},
	"attachment":            {},
	"file-history-snapshot": {},
	"ai-title":              {},
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
		if entry.IsSidechain {
			continue
		}
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

		switch entry.Type {
		case "user", "assistant":
			text := extractText(entry.Message)
			if text == "" {
				continue
			}
			session.Messages = append(session.Messages, Message{Role: entry.Type, Text: text})
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
