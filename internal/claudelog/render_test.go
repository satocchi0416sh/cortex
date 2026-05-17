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
