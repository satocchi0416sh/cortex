package launchd

import (
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
