package claudelog

import (
	"strings"

	"github.com/satocchi0416sh/cortex/internal/markdown"
)

const (
	// maxToolPayloadChars caps the size of any single tool_use input or
	// tool_result payload we embed in a Notion code block. Notion's per-rich-
	// text-item ceiling is 2000 runes; 1800 leaves headroom for the truncation
	// suffix and avoids forcing the markdown converter's chunking logic to
	// kick in inside a toggle (which would emit multiple sibling rich-text
	// items and degrade readability). Tool payloads larger than this are
	// rare; when they happen the user can still grep the raw JSONL.
	maxToolPayloadChars = 1800
)

// RenderBlocks turns a parsed session into the ordered slice of Notion
// blocks the claude-log Run path uploads. Each user/assistant message's
// text becomes one or more standard blocks (heading + paragraph / list /
// code, via the markdown converter); every assistant ToolUse becomes a
// toggle block whose children are code blocks for the input JSON and (when
// present) the matched result text.
//
// This is the AC1/AC2 implementation: tool invocations are folded under
// toggles instead of being inlined, so the Notion page stays scannable
// while still letting the user expand any single call to see exactly what
// was sent and what came back. Empty sessions emit no blocks.
func RenderBlocks(s *Session) []map[string]any {
	if s == nil {
		return nil
	}
	var out []map[string]any
	for _, m := range s.Messages {
		role := messageHeading(m.Role)
		if role == "" {
			continue
		}
		text := strings.TrimSpace(m.Text)
		if text == "" && len(m.ToolUses) == 0 {
			continue
		}
		var md strings.Builder
		md.WriteString(role)
		md.WriteString("\n\n")
		if text != "" {
			md.WriteString(text)
			md.WriteString("\n")
		}
		out = append(out, markdown.Convert([]byte(md.String()))...)
		for _, tu := range m.ToolUses {
			out = append(out, toolUseToggle(tu))
		}
	}
	return out
}

// messageHeading returns the markdown heading we prepend per role. Unknown
// roles return "" so the caller skips the message — matching the legacy
// RenderMarkdown behaviour callers already depend on in tests.
func messageHeading(role string) string {
	switch role {
	case "user":
		return "## User"
	case "assistant":
		return "## Assistant"
	}
	return ""
}

// toolUseToggle materialises a single tool invocation as a Notion toggle
// block whose summary is "🔧 tool: <name>" and whose children are code
// blocks for the JSON input and (when known) the result text. The block
// shape follows the Spike #2 layout that exercised toggle > code on
// Notion-Version 2026-03-11.
func toolUseToggle(tu ToolUse) map[string]any {
	children := []map[string]any{
		codeBlock(truncatePayload(tu.InputJSON), "json"),
	}
	if result := truncatePayload(tu.Result); result != "" {
		children = append(children, codeBlock(result, "plain text"))
	}
	name := tu.Name
	if name == "" {
		name = "(unnamed)"
	}
	return map[string]any{
		"object": "block",
		"type":   "toggle",
		"toggle": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": "🔧 tool: " + name}},
			},
			"children": children,
		},
	}
}

// codeBlock builds a Notion code block with a single rich-text item. We
// keep this local (instead of reusing the markdown package) because the
// payload is already plain text — running it through goldmark would re-
// parse JSON braces as markdown and corrupt the display.
func codeBlock(body, lang string) map[string]any {
	return map[string]any{
		"object": "block",
		"type":   "code",
		"code": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": body}},
			},
			"language": lang,
		},
	}
}

// truncatePayload clips a tool input/result string to maxToolPayloadChars
// runes, appending a "[truncated at N chars]" suffix so the Notion reader
// knows the payload was cut. Returns "" unchanged so callers can branch on
// emptiness without a special case.
func truncatePayload(s string) string {
	if s == "" {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= maxToolPayloadChars {
		return s
	}
	return string(rs[:maxToolPayloadChars]) + "\n\n[truncated at " + itoa(maxToolPayloadChars) + " chars]"
}

// itoa is a tiny local stringifier so render.go does not pull strconv just
// for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// RenderMarkdown is retained for backwards compatibility with existing
// tests that assert on the legacy markdown shape. New code paths should
// use RenderBlocks. tool_use parts are intentionally not rendered here
// because the markdown shape has no notion of toggles; callers that need
// tool visibility must use RenderBlocks.
func RenderMarkdown(s *Session) []byte {
	var b strings.Builder
	for _, m := range s.Messages {
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		head := messageHeading(m.Role)
		if head == "" {
			continue
		}
		b.WriteString(head)
		b.WriteString("\n\n")
		b.WriteString(m.Text)
		b.WriteString("\n\n")
	}
	return []byte(strings.TrimRight(b.String(), "\n"))
}
