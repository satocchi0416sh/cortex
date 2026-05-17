package claudelog

import (
	"encoding/json"
	"strings"
)

// RawEntry is the minimal projection of a Claude Code JSONL line we need to
// decide whether to keep, where it belongs in the conversation, and what
// timestamp / cwd / session metadata to extract. Anything we do not consume is
// intentionally dropped via json.RawMessage on Message and absence elsewhere.
type RawEntry struct {
	Type        string `json:"type"`
	SessionID   string `json:"sessionId"`
	IsSidechain bool   `json:"isSidechain"`
	Cwd         string `json:"cwd"`
	Timestamp   string `json:"timestamp"`
	// UUID is Claude Code's per-entry identifier. Used as the incremental
	// append cursor in internal/claudelog/sync.go; absent on older JSONL
	// schemas, in which case the appendPath treats the cursor as unknown
	// and aborts with ErrCursorStale rather than silently re-uploading.
	UUID    string          `json:"uuid"`
	Message json.RawMessage `json:"message"`
}

// extractText pulls the human-readable text out of an entry's "message" field.
// Claude Code emits message.content as either a JSON string or an array of
// typed parts ({type:"text",text:"..."} mixed with thinking/tool_use parts).
// Non-text parts and decode failures yield an empty string so callers can
// safely skip empty entries.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var envelope struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	if len(envelope.Content) == 0 {
		return ""
	}
	trimmed := strings.TrimSpace(string(envelope.Content))
	if len(trimmed) == 0 {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(envelope.Content, &s); err != nil {
			return ""
		}
		return s
	case '[':
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(envelope.Content, &parts); err != nil {
			return ""
		}
		var pieces []string
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				pieces = append(pieces, part.Text)
			}
		}
		return strings.Join(pieces, "\n")
	}
	return ""
}
