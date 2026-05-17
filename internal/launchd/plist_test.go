package launchd

import (
	"os"
	"strings"
	"testing"
)

func TestRenderPlistContainsKeys(t *testing.T) {
	body, err := RenderPlist(PlistData{
		Label:       "com.satoyoshi.cortex-sync",
		WrapperPath: "/Users/foo/bin/cortex-sync-wrapper.sh",
		HomeDir:     "/Users/foo",
		IntervalSec: 900,
		OutLogPath:  "/Users/foo/Library/Logs/cortex-sync.out.log",
		ErrLogPath:  "/Users/foo/Library/Logs/cortex-sync.err.log",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	musts := []string{
		`<key>Label</key>`,
		`<string>com.satoyoshi.cortex-sync</string>`,
		`<string>/Users/foo/bin/cortex-sync-wrapper.sh</string>`,
		`<key>StartInterval</key>`,
		`<integer>900</integer>`,
		`<key>RunAtLoad</key>`,
		`<true/>`,
		`<string>/Users/foo/Library/Logs/cortex-sync.err.log</string>`,
	}
	for _, m := range musts {
		if !strings.Contains(body, m) {
			t.Errorf("plist missing %q\n---\n%s", m, body)
		}
	}
}

func TestRenderPlistDefaultInterval(t *testing.T) {
	body, err := RenderPlist(PlistData{
		WrapperPath: "/x",
		HomeDir:     "/h",
		OutLogPath:  "/o",
		ErrLogPath:  "/e",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(body, "<integer>900</integer>") {
		t.Errorf("expected default interval 900, got: %s", body)
	}
	if !strings.Contains(body, "<string>com.satoyoshi.cortex-sync</string>") {
		t.Errorf("expected default label, got: %s", body)
	}
}

func TestRenderWrapper(t *testing.T) {
	body, err := RenderWrapper(WrapperData{
		BinaryPath: "/Users/foo/go/bin/cortex-sync",
		DatabaseID: "c556c2b71a2f46fb938787fc0f7a6016",
		ScanRoot:   "/Users/foo/Projects",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	musts := []string{
		`#!/bin/zsh`,
		`security find-generic-password -s cortex-notion -w`,
		`CORTEX_NOTION_DATABASE_ID="c556c2b71a2f46fb938787fc0f7a6016"`,
		`CORTEX_SCAN_ROOT="/Users/foo/Projects"`,
		`exec /Users/foo/go/bin/cortex-sync --once`,
	}
	for _, m := range musts {
		if !strings.Contains(body, m) {
			t.Errorf("wrapper missing %q\n---\n%s", m, body)
		}
	}
}

func TestRenderClaudeLogPlistContainsKeys(t *testing.T) {
	body, err := RenderClaudeLogPlist(PlistData{
		Label:       ClaudeLogLabel,
		WrapperPath: "/Users/foo/bin/cortex-claudelog-wrapper.sh",
		HomeDir:     "/Users/foo",
		IntervalSec: 900,
		OutLogPath:  "/Users/foo/Library/Logs/cortex-claudelog.out.log",
		ErrLogPath:  "/Users/foo/Library/Logs/cortex-claudelog.err.log",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	musts := []string{
		`<string>com.satoyoshi.cortex-claudelog</string>`,
		`<string>/Users/foo/bin/cortex-claudelog-wrapper.sh</string>`,
		`<integer>900</integer>`,
		`<string>/Users/foo/Library/Logs/cortex-claudelog.err.log</string>`,
	}
	for _, m := range musts {
		if !strings.Contains(body, m) {
			t.Errorf("claude-log plist missing %q\n---\n%s", m, body)
		}
	}
}

func TestRenderClaudeLogPlistDefaultLabel(t *testing.T) {
	body, err := RenderClaudeLogPlist(PlistData{
		WrapperPath: "/x",
		HomeDir:     "/h",
		OutLogPath:  "/o",
		ErrLogPath:  "/e",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(body, "<string>com.satoyoshi.cortex-claudelog</string>") {
		t.Errorf("expected default claude-log label, got: %s", body)
	}
	if !strings.Contains(body, "<integer>900</integer>") {
		t.Errorf("expected default interval 900, got: %s", body)
	}
}

func TestRenderClaudeLogWrapper(t *testing.T) {
	body, err := RenderClaudeLogWrapper(ClaudeLogWrapperData{
		BinaryPath:         "/Users/foo/go/bin/cortex-sync",
		DatabaseID:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ClaudeProjectsRoot: "/Users/foo/.claude/projects",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	musts := []string{
		`#!/bin/zsh`,
		`security find-generic-password -s cortex-notion -w`,
		`CORTEX_CLAUDELOG_DATABASE_ID="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`,
		`CORTEX_CLAUDE_PROJECTS_ROOT="/Users/foo/.claude/projects"`,
		`exec /Users/foo/go/bin/cortex-sync claude-log --all`,
	}
	for _, m := range musts {
		if !strings.Contains(body, m) {
			t.Errorf("claude-log wrapper missing %q\n---\n%s", m, body)
		}
	}
}

func TestRenderClaudeLogWrapper_OmitsProjectsRootWhenEmpty(t *testing.T) {
	body, err := RenderClaudeLogWrapper(ClaudeLogWrapperData{
		BinaryPath: "/Users/foo/go/bin/cortex-sync",
		DatabaseID: "abc",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(body, "CORTEX_CLAUDE_PROJECTS_ROOT") {
		t.Errorf("expected CORTEX_CLAUDE_PROJECTS_ROOT to be omitted when empty, got:\n%s", body)
	}
	if !strings.Contains(body, "claude-log --all") {
		t.Errorf("expected claude-log --all invocation, got:\n%s", body)
	}
}

func TestDefaultPaths_ClaudeLogFields(t *testing.T) {
	p, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	if p.ClaudeLogPlistPath == "" || !strings.Contains(p.ClaudeLogPlistPath, "com.satoyoshi.cortex-claudelog.plist") {
		t.Errorf("ClaudeLogPlistPath unexpected: %q", p.ClaudeLogPlistPath)
	}
	if p.ClaudeLogWrapperPath == "" || !strings.Contains(p.ClaudeLogWrapperPath, "cortex-claudelog-wrapper.sh") {
		t.Errorf("ClaudeLogWrapperPath unexpected: %q", p.ClaudeLogWrapperPath)
	}
	if p.ClaudeLogOutLogPath == "" || !strings.Contains(p.ClaudeLogOutLogPath, "cortex-claudelog.out.log") {
		t.Errorf("ClaudeLogOutLogPath unexpected: %q", p.ClaudeLogOutLogPath)
	}
	if p.ClaudeLogErrLogPath == "" || !strings.Contains(p.ClaudeLogErrLogPath, "cortex-claudelog.err.log") {
		t.Errorf("ClaudeLogErrLogPath unexpected: %q", p.ClaudeLogErrLogPath)
	}
	// Backwards-compat: markdown-sync fields must remain unchanged.
	if !strings.Contains(p.PlistPath, "com.satoyoshi.cortex-sync.plist") {
		t.Errorf("markdown PlistPath drifted: %q", p.PlistPath)
	}
	if !strings.Contains(p.WrapperPath, "cortex-sync-wrapper.sh") {
		t.Errorf("markdown WrapperPath drifted: %q", p.WrapperPath)
	}
}

func TestWriteClaudeLogFiles_WritesBoth(t *testing.T) {
	tmp := t.TempDir()
	paths := Paths{
		HomeDir:              tmp,
		PlistPath:            tmp + "/LaunchAgents/" + MarkdownLabel + ".plist",
		WrapperPath:          tmp + "/bin/cortex-sync-wrapper.sh",
		OutLogPath:           tmp + "/Logs/cortex-sync.out.log",
		ErrLogPath:           tmp + "/Logs/cortex-sync.err.log",
		LogDir:               tmp + "/Logs",
		WrapperDir:           tmp + "/bin",
		ClaudeLogPlistPath:   tmp + "/LaunchAgents/" + ClaudeLogLabel + ".plist",
		ClaudeLogWrapperPath: tmp + "/bin/cortex-claudelog-wrapper.sh",
		ClaudeLogOutLogPath:  tmp + "/Logs/cortex-claudelog.out.log",
		ClaudeLogErrLogPath:  tmp + "/Logs/cortex-claudelog.err.log",
	}
	pd := PlistData{
		Label:       ClaudeLogLabel,
		WrapperPath: paths.ClaudeLogWrapperPath,
		HomeDir:     tmp,
		IntervalSec: 900,
		OutLogPath:  paths.ClaudeLogOutLogPath,
		ErrLogPath:  paths.ClaudeLogErrLogPath,
	}
	wd := ClaudeLogWrapperData{
		BinaryPath: "/usr/local/bin/cortex-sync",
		DatabaseID: "deadbeef",
	}
	if err := WriteClaudeLogFiles(paths, pd, wd); err != nil {
		t.Fatalf("WriteClaudeLogFiles: %v", err)
	}
	// idempotent re-write (overwrite) must succeed
	if err := WriteClaudeLogFiles(paths, pd, wd); err != nil {
		t.Fatalf("WriteClaudeLogFiles second pass: %v", err)
	}
	pb, err := os.ReadFile(paths.ClaudeLogPlistPath)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !strings.Contains(string(pb), ClaudeLogLabel) {
		t.Errorf("plist missing label, got:\n%s", pb)
	}
	wb, err := os.ReadFile(paths.ClaudeLogWrapperPath)
	if err != nil {
		t.Fatalf("read wrapper: %v", err)
	}
	if !strings.Contains(string(wb), "claude-log --all") {
		t.Errorf("wrapper missing claude-log --all, got:\n%s", wb)
	}
}

func TestLabelAliasUnchanged(t *testing.T) {
	if Label != MarkdownLabel {
		t.Errorf("Label alias drifted: Label=%q MarkdownLabel=%q", Label, MarkdownLabel)
	}
	if Label != "com.satoyoshi.cortex-sync" {
		t.Errorf("Label changed value: %q", Label)
	}
}
