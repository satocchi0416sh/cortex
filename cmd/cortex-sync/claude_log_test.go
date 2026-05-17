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
		t.Errorf("expected exit 2 when --session and --all both missing, got %d", code)
	}
}

func TestRunClaudeLog_SessionAndAllConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CORTEX_NOTION_TOKEN", "tok")
	t.Setenv("CORTEX_CLAUDELOG_DATABASE_ID", "db")
	t.Setenv("CORTEX_CLAUDE_PROJECTS_ROOT", t.TempDir())
	t.Setenv("CORTEX_NOTION_DATABASE_ID", "")
	t.Setenv("CORTEX_STATE_FILE", "")
	t.Setenv("CORTEX_SCAN_ROOT", "")

	code := runClaudeLog(context.Background(), []string{"--session", "abc", "--all"})
	if code != 2 {
		t.Errorf("expected exit 2 when --session and --all are both set, got %d", code)
	}
}

func TestRunClaudeLog_AllEmptyRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CORTEX_NOTION_TOKEN", "tok")
	t.Setenv("CORTEX_CLAUDELOG_DATABASE_ID", "db")
	t.Setenv("CORTEX_CLAUDE_PROJECTS_ROOT", t.TempDir())
	t.Setenv("CORTEX_NOTION_DATABASE_ID", "")
	t.Setenv("CORTEX_STATE_FILE", "")
	t.Setenv("CORTEX_SCAN_ROOT", "")

	// dry-run so we never reach the Notion gateway; --all on an empty root
	// should report zero work and exit 0.
	code := runClaudeLog(context.Background(), []string{"--all", "--dry-run"})
	if code != 0 {
		t.Errorf("expected exit 0 for --all on empty root (dry-run), got %d", code)
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
