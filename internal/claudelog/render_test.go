package claudelog

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_OrderAndHeadings(t *testing.T) {
	s := &Session{
		Messages: []Message{
			{Role: "user", Text: "first"},
			{Role: "assistant", Text: "second"},
			{Role: "user", Text: "third"},
		},
	}
	got := string(RenderMarkdown(s))
	want := "## User\n\nfirst\n\n## Assistant\n\nsecond\n\n## User\n\nthird"
	if got != want {
		t.Errorf("RenderMarkdown =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderMarkdown_SkipsBlankAndUnknownRoles(t *testing.T) {
	s := &Session{
		Messages: []Message{
			{Role: "user", Text: ""},
			{Role: "system", Text: "ignored"},
			{Role: "user", Text: "kept"},
			{Role: "assistant", Text: "  "},
		},
	}
	got := string(RenderMarkdown(s))
	if !strings.Contains(got, "kept") {
		t.Errorf("kept message missing: %q", got)
	}
	if strings.Contains(got, "ignored") {
		t.Errorf("unknown role leaked: %q", got)
	}
	if strings.Count(got, "## ") != 1 {
		t.Errorf("expected one heading, got %q", got)
	}
}

func TestRenderMarkdown_NoMessages(t *testing.T) {
	got := RenderMarkdown(&Session{})
	if len(got) != 0 {
		t.Errorf("expected empty output for no messages, got %q", string(got))
	}
}

func TestRenderBlocks_NilSession(t *testing.T) {
	if got := RenderBlocks(nil); got != nil {
		t.Errorf("RenderBlocks(nil) = %v, want nil", got)
	}
}

func TestRenderBlocks_UserAssistantOnly(t *testing.T) {
	s := &Session{
		Messages: []Message{
			{Role: "user", Text: "ping"},
			{Role: "assistant", Text: "pong"},
		},
	}
	got := RenderBlocks(s)
	if len(got) == 0 {
		t.Fatalf("expected blocks, got 0")
	}
	for _, b := range got {
		if b["type"] == "toggle" {
			t.Errorf("expected no toggle blocks when ToolUses empty, got %+v", b)
		}
	}
}

func TestRenderBlocks_AssistantWithToolUses(t *testing.T) {
	s := &Session{
		Messages: []Message{
			{Role: "user", Text: "list files"},
			{
				Role: "assistant",
				Text: "sure",
				ToolUses: []ToolUse{
					{ID: "t1", Name: "Bash", InputJSON: `{"command":"ls"}`, Result: "total 0"},
					{ID: "t2", Name: "Read", InputJSON: `{"path":"/x"}`, Result: "contents"},
				},
			},
		},
	}
	got := RenderBlocks(s)

	var toggles []map[string]any
	for _, b := range got {
		if b["type"] == "toggle" {
			toggles = append(toggles, b)
		}
	}
	if len(toggles) != 2 {
		t.Fatalf("expected 2 toggle blocks, got %d (all blocks=%d)", len(toggles), len(got))
	}

	// Toggle 1: Bash
	tg := toggles[0]["toggle"].(map[string]any)
	rt := tg["rich_text"].([]map[string]any)
	if len(rt) != 1 {
		t.Fatalf("toggle rich_text len = %d, want 1", len(rt))
	}
	summary := rt[0]["text"].(map[string]any)["content"].(string)
	if !strings.Contains(summary, "Bash") || !strings.Contains(summary, "🔧") {
		t.Errorf("toggle[0] summary = %q, want 🔧 + tool name", summary)
	}
	children := tg["children"].([]map[string]any)
	if len(children) != 2 {
		t.Fatalf("toggle[0] children len = %d, want 2 (input + result)", len(children))
	}
	// Child 0: input code block, language json
	c0 := children[0]
	if c0["type"] != "code" {
		t.Errorf("toggle[0] child[0] type = %v, want code", c0["type"])
	}
	c0body := c0["code"].(map[string]any)
	if c0body["language"] != "json" {
		t.Errorf("toggle[0] child[0] language = %v, want json", c0body["language"])
	}
	if !strings.Contains(stringifyBlocks([]map[string]any{c0}), `"command":"ls"`) {
		t.Errorf("toggle[0] child[0] missing command input")
	}
	// Child 1: result code block, language plain text
	c1 := children[1]
	c1body := c1["code"].(map[string]any)
	if c1body["language"] != "plain text" {
		t.Errorf("toggle[0] child[1] language = %v, want 'plain text'", c1body["language"])
	}
	if !strings.Contains(stringifyBlocks([]map[string]any{c1}), "total 0") {
		t.Errorf("toggle[0] child[1] missing result body")
	}
}

func TestRenderBlocks_ToolUseWithoutResult(t *testing.T) {
	s := &Session{
		Messages: []Message{
			{
				Role:     "assistant",
				Text:     "calling",
				ToolUses: []ToolUse{{ID: "t1", Name: "Bash", InputJSON: `{"command":"x"}`}},
			},
		},
	}
	got := RenderBlocks(s)
	var toggle map[string]any
	for _, b := range got {
		if b["type"] == "toggle" {
			toggle = b
			break
		}
	}
	if toggle == nil {
		t.Fatalf("expected toggle block even without result")
	}
	children := toggle["toggle"].(map[string]any)["children"].([]map[string]any)
	if len(children) != 1 {
		t.Errorf("children len = %d, want 1 (input only, no result block)", len(children))
	}
}

func TestRenderBlocks_LongInputTruncated(t *testing.T) {
	long := strings.Repeat("x", maxToolPayloadChars+500)
	s := &Session{
		Messages: []Message{
			{
				Role:     "assistant",
				ToolUses: []ToolUse{{ID: "t1", Name: "Bash", InputJSON: long}},
			},
		},
	}
	got := RenderBlocks(s)
	var toggle map[string]any
	for _, b := range got {
		if b["type"] == "toggle" {
			toggle = b
			break
		}
	}
	if toggle == nil {
		t.Fatal("expected toggle")
	}
	children := toggle["toggle"].(map[string]any)["children"].([]map[string]any)
	body := stringifyBlocks([]map[string]any{children[0]})
	if !strings.Contains(body, "[truncated at") {
		t.Errorf("expected truncation marker, got %q...", body[:min(120, len(body))])
	}
}

func TestRenderBlocks_AssistantWithToolUsesNoText(t *testing.T) {
	s := &Session{
		Messages: []Message{
			{
				Role:     "assistant",
				ToolUses: []ToolUse{{ID: "t1", Name: "Bash", InputJSON: "{}"}},
			},
		},
	}
	got := RenderBlocks(s)
	hasToggle := false
	for _, b := range got {
		if b["type"] == "toggle" {
			hasToggle = true
		}
	}
	if !hasToggle {
		t.Errorf("assistant with only tool_uses (no text) must still emit toggle, got %+v", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
