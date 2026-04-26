package markdown

import (
	"strings"
	"testing"
)

func TestConvertCases(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantTypes []string
	}{
		{"heading1", "# hello\n", []string{"heading_1"}},
		{"heading2", "## hello\n", []string{"heading_2"}},
		{"heading3", "### hello\n", []string{"heading_3"}},
		{"heading4_clamped_to_3", "#### hello\n", []string{"heading_3"}},
		{"paragraph", "just a line\n", []string{"paragraph"}},
		{"bullet", "- a\n- b\n", []string{"bulleted_list_item", "bulleted_list_item"}},
		{"numbered", "1. a\n2. b\n", []string{"numbered_list_item", "numbered_list_item"}},
		{"todo", "- [ ] open\n- [x] done\n", []string{"to_do", "to_do"}},
		{"fenced_code", "```go\nfmt.Println(1)\n```\n", []string{"code"}},
		{"quote", "> hi\n", []string{"quote"}},
		{"divider", "---\n", []string{"divider"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Convert([]byte(tc.input))
			if len(got) != len(tc.wantTypes) {
				t.Fatalf("got %d blocks, want %d: %#v", len(got), len(tc.wantTypes), got)
			}
			for i, b := range got {
				if b["type"] != tc.wantTypes[i] {
					t.Errorf("block %d type=%v want=%v", i, b["type"], tc.wantTypes[i])
				}
			}
		})
	}
}

func TestInlineEmphasisAndCode(t *testing.T) {
	blocks := Convert([]byte("a **bold** and *italic* and `code`\n"))
	if len(blocks) != 1 {
		t.Fatalf("blocks=%d", len(blocks))
	}
	rt := blocks[0]["paragraph"].(map[string]any)["rich_text"].([]map[string]any)
	var hasBold, hasItalic, hasCode bool
	for _, item := range rt {
		ann, _ := item["annotations"].(map[string]any)
		if ann["bold"] == true {
			hasBold = true
		}
		if ann["italic"] == true {
			hasItalic = true
		}
		if ann["code"] == true {
			hasCode = true
		}
	}
	if !hasBold || !hasItalic || !hasCode {
		t.Errorf("annotations missing: bold=%v italic=%v code=%v", hasBold, hasItalic, hasCode)
	}
}

func TestInlineLink(t *testing.T) {
	blocks := Convert([]byte("see [Notion](https://www.notion.so/) docs\n"))
	rt := blocks[0]["paragraph"].(map[string]any)["rich_text"].([]map[string]any)
	var found bool
	for _, item := range rt {
		txt, _ := item["text"].(map[string]any)
		if link, ok := txt["link"].(map[string]any); ok {
			if link["url"] == "https://www.notion.so/" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("link not found in rich_text: %#v", rt)
	}
}

func TestLongTextChunked(t *testing.T) {
	body := strings.Repeat("a", 4500) + "\n"
	blocks := Convert([]byte(body))
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	rt := blocks[0]["paragraph"].(map[string]any)["rich_text"].([]map[string]any)
	if len(rt) < 3 {
		t.Errorf("expected at least 3 chunks, got %d", len(rt))
	}
	for i, item := range rt {
		s := item["text"].(map[string]any)["content"].(string)
		if len([]rune(s)) > maxRichTextLen {
			t.Errorf("chunk %d too long: %d", i, len([]rune(s)))
		}
	}
}

func TestTodoChecked(t *testing.T) {
	blocks := Convert([]byte("- [x] done\n"))
	if blocks[0]["type"] != "to_do" {
		t.Fatalf("type=%v", blocks[0]["type"])
	}
	body := blocks[0]["to_do"].(map[string]any)
	if body["checked"] != true {
		t.Errorf("expected checked=true, got %v", body["checked"])
	}
}

func TestCodeLanguageNormalized(t *testing.T) {
	blocks := Convert([]byte("```sh\necho hi\n```\n"))
	body := blocks[0]["code"].(map[string]any)
	if body["language"] != "shell" {
		t.Errorf("expected shell, got %v", body["language"])
	}
}

func TestUnknownLanguageFallback(t *testing.T) {
	blocks := Convert([]byte("```\nplain stuff\n```\n"))
	body := blocks[0]["code"].(map[string]any)
	if body["language"] != "plain text" {
		t.Errorf("expected plain text, got %v", body["language"])
	}
}

func TestUnknownNamedLanguageFallsBackToPlainText(t *testing.T) {
	blocks := Convert([]byte("```env\nFOO=bar\n```\n"))
	body := blocks[0]["code"].(map[string]any)
	if body["language"] != "plain text" {
		t.Errorf("expected plain text for unknown lang, got %v", body["language"])
	}
}

func TestNotionLangPassesThrough(t *testing.T) {
	cases := map[string]string{
		"go":         "go",
		"```python":  "python",
		"```TSX":     "typescript",
		"dockerfile": "docker",
		"yaml":       "yaml",
	}
	for raw, want := range cases {
		got := normalizeLang(strings.TrimPrefix(raw, "```"))
		if got != want {
			t.Errorf("normalizeLang(%q)=%q want %q", raw, got, want)
		}
	}
}
