package main

import (
	"context"
	"testing"
)

func TestRunClaudeLog_MissingSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CORTEX_NOTION_TOKEN", "tok")
	t.Setenv("CORTEX_CLAUDELOG_DATABASE_ID", "db")
	t.Setenv("CORTEX_CLAUDE_PROJECTS_ROOT", t.TempDir())
	t.Setenv("CORTEX_NOTION_DATABASE_ID", "")
	t.Setenv("CORTEX_STATE_FILE", "")
	t.Setenv("CORTEX_SCAN_ROOT", "")

	code := runClaudeLog(context.Background(), nil)
	if code != 2 {
		t.Errorf("expected exit 2 when --session missing, got %d", code)
	}
}

func TestRunClaudeLog_MissingEnv(t *testing.T) {
	// Clear all relevant env so config load fails.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CORTEX_NOTION_TOKEN", "")
	t.Setenv("CORTEX_CLAUDELOG_DATABASE_ID", "")
	t.Setenv("CORTEX_CLAUDE_PROJECTS_ROOT", t.TempDir())
	t.Setenv("CORTEX_NOTION_DATABASE_ID", "")
	t.Setenv("CORTEX_STATE_FILE", "")
	t.Setenv("CORTEX_SCAN_ROOT", "")

	code := runClaudeLog(context.Background(), []string{"--session", "abc"})
	if code != 2 {
		t.Errorf("expected exit 2 on missing token+db env, got %d", code)
	}
}
