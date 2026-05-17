package claudelog

import "strings"

// RenderMarkdown turns a parsed session's messages into a markdown document
// formatted as alternating "## User" / "## Assistant" sections. The output
// drives notion-block conversion via the existing markdown package, so it
// must be plain GFM. Empty messages are skipped defensively even though
// ParseSession already filters them.
func RenderMarkdown(s *Session) []byte {
	var b strings.Builder
	for _, m := range s.Messages {
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		switch m.Role {
		case "user":
			b.WriteString("## User\n\n")
		case "assistant":
			b.WriteString("## Assistant\n\n")
		default:
			continue
		}
		b.WriteString(m.Text)
		b.WriteString("\n\n")
	}
	return []byte(strings.TrimRight(b.String(), "\n"))
}
