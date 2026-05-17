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
	UUID string `json:"uuid"`
	// AITitle carries the one-line session summary Claude Code emits as a
	// top-level field on entries of type "ai-title". Captured into
	// Session.AITitle so the Notion "Name" property prefers it over the
	// synthetic fallback. Empty for any entry that is not an ai-title.
	AITitle string          `json:"aiTitle"`
	Message json.RawMessage `json:"message"`
}

// ToolUse captures a tool invocation extracted from an assistant message's
// content array. ID lets the parser pair it with the matching tool_result
// part that arrives in a subsequent user message; Result is filled by the
// parser once the pair is resolved, or left empty when no matching result
// is present (timeouts, interrupts, JSONL truncation).
//
// InputJSON is the raw JSON-serialised tool input verbatim from the JSONL
// (defaulting to "{}" when no input was sent) so the renderer can embed it
// in a Notion code block without re-marshalling and losing key order.
type ToolUse struct {
	ID        string
	Name      string
	InputJSON string
	Result    string
}

// contentPart is the parser-internal union over the three content element
// shapes claude-log cares about: plain assistant text, an assistant
// tool_use invocation, and a user tool_result reply. It exists so a single
// pass over the content array can extract everything ParseSession needs,
// instead of decoding the same JSON three times.
type contentPart struct {
	Type      string
	Text      string // type == "text"
	ID        string // type == "tool_use"
	Name      string // type == "tool_use"
	InputJSON string // type == "tool_use"; "{}" when input is absent or empty
	ToolUseID string // type == "tool_result"
	Result    string // type == "tool_result"; flattened text content
}

// extractParts pulls the typed parts out of an entry's "message" field.
// Claude Code emits message.content as either a JSON string or an array of
// typed parts ({type:"text",text:"..."} mixed with thinking / tool_use /
// tool_result parts). A string content is normalised to a single
// text part so callers can iterate uniformly. Decode failures yield nil so
// callers safely skip the entry.
func extractParts(raw json.RawMessage) []contentPart {
	if len(raw) == 0 {
		return nil
	}
	var envelope struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	if len(envelope.Content) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(envelope.Content))
	if len(trimmed) == 0 {
		return nil
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(envelope.Content, &s); err != nil {
			return nil
		}
		return []contentPart{{Type: "text", Text: s}}
	case '[':
		var raws []json.RawMessage
		if err := json.Unmarshal(envelope.Content, &raws); err != nil {
			return nil
		}
		parts := make([]contentPart, 0, len(raws))
		for _, item := range raws {
			p, ok := decodeContentPart(item)
			if !ok {
				continue
			}
			parts = append(parts, p)
		}
		return parts
	}
	return nil
}

// decodeContentPart inspects a single content array element and returns the
// typed extraction. The ok return is false when the element is a type we
// intentionally drop (e.g. "thinking") or fails to decode.
func decodeContentPart(item json.RawMessage) (contentPart, bool) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(item, &head); err != nil {
		return contentPart{}, false
	}
	switch head.Type {
	case "text":
		var t struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(item, &t); err != nil {
			return contentPart{}, false
		}
		if t.Text == "" {
			return contentPart{}, false
		}
		return contentPart{Type: "text", Text: t.Text}, true
	case "tool_use":
		var tu struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(item, &tu); err != nil {
			return contentPart{}, false
		}
		// "{}" default keeps the renderer's code block non-empty when the
		// tool was invoked with no arguments, which is more readable than
		// an empty Notion code block.
		input := strings.TrimSpace(string(tu.Input))
		if input == "" || input == "null" {
			input = "{}"
		}
		return contentPart{
			Type:      "tool_use",
			ID:        tu.ID,
			Name:      tu.Name,
			InputJSON: input,
		}, true
	case "tool_result":
		var tr struct {
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(item, &tr); err != nil {
			return contentPart{}, false
		}
		return contentPart{
			Type:      "tool_result",
			ToolUseID: tr.ToolUseID,
			Result:    flattenToolResultContent(tr.Content),
		}, true
	}
	return contentPart{}, false
}

// flattenToolResultContent normalises a tool_result's content into a single
// plain string. Claude Code emits this field as either a JSON string or an
// array of {type:"text", text:"..."} objects; image / non-text parts are
// dropped because the toggle renderer only embeds text code blocks.
func flattenToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ""
		}
		return s
	case '[':
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &parts); err != nil {
			return ""
		}
		var pieces []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				pieces = append(pieces, p.Text)
			}
		}
		return strings.Join(pieces, "\n")
	}
	return ""
}
