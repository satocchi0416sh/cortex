package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultScanRoot(t *testing.T) {
	t.Run("home set", func(t *testing.T) {
		t.Setenv("HOME", "/tmp/home-set-test")
		got := DefaultScanRoot()
		want := filepath.Join("/tmp/home-set-test", "Projects")
		if got != want {
			t.Fatalf("DefaultScanRoot() = %q, want %q", got, want)
		}
	})

	t.Run("home unset", func(t *testing.T) {
		t.Setenv("HOME", "")
		got := DefaultScanRoot()
		if got != "" {
			t.Fatalf("DefaultScanRoot() with empty HOME = %q, want empty string", got)
		}
	})
}

func TestDefaultClaudeProjectsRoot(t *testing.T) {
	t.Run("home set", func(t *testing.T) {
		t.Setenv("HOME", "/tmp/claude-home-set")
		got := DefaultClaudeProjectsRoot()
		want := filepath.Join("/tmp/claude-home-set", ".claude", "projects")
		if got != want {
			t.Fatalf("DefaultClaudeProjectsRoot() = %q, want %q", got, want)
		}
	})

	t.Run("home unset", func(t *testing.T) {
		t.Setenv("HOME", "")
		got := DefaultClaudeProjectsRoot()
		if got != "" {
			t.Fatalf("DefaultClaudeProjectsRoot() with empty HOME = %q, want empty string", got)
		}
	})
}

func TestLoadForClaudeLog_envApplied(t *testing.T) {
	t.Setenv("HOME", "/tmp/claudelog-env-home")
	t.Setenv("CORTEX_NOTION_TOKEN", "tok-env")
	t.Setenv("CORTEX_CLAUDELOG_DATABASE_ID", "claudelog-db-env")
	t.Setenv("CORTEX_CLAUDE_PROJECTS_ROOT", "/tmp/claude-projects-env")
	// markdown-sync env vars should be ignored for claude-log validation.
	t.Setenv("CORTEX_NOTION_DATABASE_ID", "")
	t.Setenv("CORTEX_SCAN_ROOT", "")
	t.Setenv("CORTEX_STATE_FILE", "")

	cfg, err := LoadForClaudeLog(Flags{})
	if err != nil {
		t.Fatalf("LoadForClaudeLog: %v", err)
	}
	if cfg.NotionToken != "tok-env" {
		t.Errorf("NotionToken = %q", cfg.NotionToken)
	}
	if cfg.ClaudeLogDatabaseID != "claudelog-db-env" {
		t.Errorf("ClaudeLogDatabaseID = %q", cfg.ClaudeLogDatabaseID)
	}
	if cfg.ClaudeProjectsRoot != "/tmp/claude-projects-env" {
		t.Errorf("ClaudeProjectsRoot = %q", cfg.ClaudeProjectsRoot)
	}
	wantState := filepath.Join("/tmp/claudelog-env-home", ".cortex", "claudelog_state.json")
	if cfg.StateFile != wantState {
		t.Errorf("StateFile default = %q want %q", cfg.StateFile, wantState)
	}
}

func TestLoadForClaudeLog_missingRequired(t *testing.T) {
	t.Setenv("HOME", "/tmp/claudelog-missing-home")
	t.Setenv("CORTEX_NOTION_TOKEN", "")
	t.Setenv("CORTEX_CLAUDELOG_DATABASE_ID", "")
	t.Setenv("CORTEX_CLAUDE_PROJECTS_ROOT", "/tmp/cpr")
	t.Setenv("CORTEX_NOTION_DATABASE_ID", "")
	t.Setenv("CORTEX_SCAN_ROOT", "")
	t.Setenv("CORTEX_STATE_FILE", "")

	_, err := LoadForClaudeLog(Flags{})
	if err == nil {
		t.Fatalf("expected error when token and DB ID missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "CORTEX_NOTION_TOKEN") {
		t.Errorf("error missing CORTEX_NOTION_TOKEN: %s", msg)
	}
	if !strings.Contains(msg, "CORTEX_CLAUDELOG_DATABASE_ID") {
		t.Errorf("error missing CORTEX_CLAUDELOG_DATABASE_ID: %s", msg)
	}
}

func TestLoadForClaudeLog_dryRunSkipsTokenAndDB(t *testing.T) {
	t.Setenv("HOME", "/tmp/claudelog-dryrun-home")
	t.Setenv("CORTEX_NOTION_TOKEN", "")
	t.Setenv("CORTEX_CLAUDELOG_DATABASE_ID", "")
	t.Setenv("CORTEX_CLAUDE_PROJECTS_ROOT", "/tmp/cpr")
	t.Setenv("CORTEX_NOTION_DATABASE_ID", "")
	t.Setenv("CORTEX_SCAN_ROOT", "")
	t.Setenv("CORTEX_STATE_FILE", "")

	cfg, err := LoadForClaudeLog(Flags{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run LoadForClaudeLog: %v", err)
	}
	if cfg.NotionToken != "" || cfg.ClaudeLogDatabaseID != "" {
		t.Errorf("dry-run should not fabricate creds: token=%q db=%q", cfg.NotionToken, cfg.ClaudeLogDatabaseID)
	}
}
