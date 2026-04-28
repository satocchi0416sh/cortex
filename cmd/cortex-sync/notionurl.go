package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	dbIDHexRe   = regexp.MustCompile(`[0-9a-fA-F]{32}`)
	dbIDDashRe  = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

// extractDatabaseID parses a raw Notion DB ID, dashed UUID, or notion.so URL and
// returns the canonical 32-hex-char database ID. The view query parameter (?v=)
// is intentionally stripped before scanning to avoid matching the view ID.
func extractDatabaseID(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("empty input")
	}

	// Strip query string ("?v=..." view ID, etc.)
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}

	// Strip URL fragment.
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}

	// Try dashed UUID first; convert to 32-hex.
	if m := dbIDDashRe.FindString(s); m != "" {
		return strings.ToLower(strings.ReplaceAll(m, "-", "")), nil
	}
	if m := dbIDHexRe.FindString(s); m != "" {
		return strings.ToLower(m), nil
	}
	return "", fmt.Errorf("could not find a 32-hex Notion database ID in %q", input)
}
