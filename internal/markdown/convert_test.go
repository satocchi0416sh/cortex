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


func TestGFMTable(t *testing.T) {
	src := `| Name | Type | Note |
|---|---|---|
| foo | int | first |
| bar | str | second |
`
	blocks := Convert([]byte(src))
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0]["type"] != "table" {
		t.Fatalf("expected table block, got %v", blocks[0]["type"])
	}
	body := blocks[0]["table"].(map[string]any)
	if body["table_width"] != 3 {
		t.Errorf("expected width=3, got %v", body["table_width"])
	}
	if body["has_column_header"] != true {
		t.Errorf("expected has_column_header=true")
	}
	rows := body["children"].([]Block)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (header + 2 body), got %d", len(rows))
	}
	for i, r := range rows {
		if r["type"] != "table_row" {
			t.Errorf("row %d type=%v", i, r["type"])
		}
		cells := r["table_row"].(map[string]any)["cells"].([]any)
		if len(cells) != 3 {
			t.Errorf("row %d has %d cells, want 3", i, len(cells))
		}
	}
}

func TestTableWithRaggedRowsPaddedToWidth(t *testing.T) {
	src := `| A | B | C |
|---|---|---|
| 1 | 2 | 3 |
| only-one |
`
	blocks := Convert([]byte(src))
	body := blocks[0]["table"].(map[string]any)
	if body["table_width"] != 3 {
		t.Errorf("width=%v want 3", body["table_width"])
	}
	rows := body["children"].([]Block)
	for i, r := range rows {
		cells := r["table_row"].(map[string]any)["cells"].([]any)
		if len(cells) != 3 {
			t.Errorf("row %d cells=%d want 3 (padded)", i, len(cells))
		}
	}
}

// linkURLs extracts every emitted rich_text link URL across all blocks,
// recursing into nested children (e.g. list items, quotes).
func linkURLs(blocks []Block) []string {
	var urls []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case []map[string]any:
			for _, m := range t {
				walk(m)
			}
		case map[string]any:
			if txt, ok := t["text"].(map[string]any); ok {
				if lnk, ok := txt["link"].(map[string]any); ok {
					if u, ok := lnk["url"].(string); ok {
						urls = append(urls, u)
					}
				}
			}
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	for _, b := range blocks {
		walk(map[string]any(b))
	}
	return urls
}

// TestSanitizeLinkURL_DropsInvalid guards against the Notion 400
// "Invalid URL for link" regression: relative destinations, fragment anchors,
// and scheme-less email autolinks must not be emitted as rich_text links,
// while genuine absolute URLs are preserved.
func TestSanitizeLinkURL_DropsInvalid(t *testing.T) {
	dropped := []struct{ name, input string }{
		{"dot_relative", "see [log](.) here\n"},
		{"parent_relative", "see [doc](../guide.md) here\n"},
		{"fragment_anchor", "see [top](#section) here\n"},
		{"bare_email", "ping no-reply@accounts.nintendo.com now\n"},
		{"email_false_positive", "metric Recall@IoU0.7 reported\n"},
	}
	for _, tc := range dropped {
		t.Run("drop_"+tc.name, func(t *testing.T) {
			if urls := linkURLs(Convert([]byte(tc.input))); len(urls) != 0 {
				t.Errorf("expected no links, got %v", urls)
			}
		})
	}

	kept := []struct{ name, input, want string }{
		{"https", "real https://example.com/x link\n", "https://example.com/x"},
		{"http", "real http://example.com link\n", "http://example.com"},
		{"www_gets_scheme", "visit www.example.com today\n", "http://www.example.com"},
		{"explicit_mailto", "[mail](mailto:a@b.com)\n", "mailto:a@b.com"},
	}
	for _, tc := range kept {
		t.Run("keep_"+tc.name, func(t *testing.T) {
			urls := linkURLs(Convert([]byte(tc.input)))
			found := false
			for _, u := range urls {
				if u == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("expected link %q, got %v", tc.want, urls)
			}
		})
	}
}

// TestSanitizeLinkURL_Unit exercises the helper directly.
func TestSanitizeLinkURL_Unit(t *testing.T) {
	bad := []string{"", ".", "  ", "../x.md", "#a", "foo@bar.com", "Recall@IoU0.7", "http://", "https://"}
	for _, b := range bad {
		if u, ok := sanitizeLinkURL(b); ok {
			t.Errorf("sanitizeLinkURL(%q) = (%q, true), want ok=false", b, u)
		}
	}
	good := map[string]string{
		"https://x.io/a": "https://x.io/a",
		"http://x.io":    "http://x.io",
		"mailto:a@b.com": "mailto:a@b.com",
		"tel:+15551234":  "tel:+15551234",
		" https://x.io ": "https://x.io",
	}
	for in, want := range good {
		if u, ok := sanitizeLinkURL(in); !ok || u != want {
			t.Errorf("sanitizeLinkURL(%q) = (%q, %v), want (%q, true)", in, u, ok, want)
		}
	}
}
