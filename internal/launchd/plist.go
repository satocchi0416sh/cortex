// Package launchd provides idempotent install / uninstall helpers for the
// cortex-sync macOS launchd jobs (LaunchAgents). Two jobs are supported:
// the markdown sync job (com.satoyoshi.cortex-sync) and the Claude Code
// conversation-log sync job (com.satoyoshi.cortex-claudelog). Each job's
// plist + wrapper script are embedded as text/template at compile time so
// the binary is self-contained.
package launchd

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/cortex-sync.plist.tmpl
var plistTmplSrc string

//go:embed templates/cortex-sync-wrapper.sh.tmpl
var wrapperTmplSrc string

//go:embed templates/cortex-claudelog.plist.tmpl
var claudeLogPlistTmplSrc string

//go:embed templates/cortex-claudelog-wrapper.sh.tmpl
var claudeLogWrapperTmplSrc string

// MarkdownLabel is the launchd job label for the markdown sync job
// (~/Projects/*/.serena/memories/*.md → Notion). Kept stable across versions
// so reinstall is idempotent.
const MarkdownLabel = "com.satoyoshi.cortex-sync"

// ClaudeLogLabel is the launchd job label for the Claude Code conversation
// log sync job. Distinct from MarkdownLabel so the two jobs install / start /
// stop independently.
const ClaudeLogLabel = "com.satoyoshi.cortex-claudelog"

// Label is retained as an alias of MarkdownLabel for backwards compatibility
// with code that referenced the old single-job constant.
const Label = MarkdownLabel

// PlistData drives the plist template render. Shared between both jobs because
// the plist template itself is structurally identical (label, wrapper path,
// interval, log paths) — what differs is the wrapper script invoked.
type PlistData struct {
	Label       string
	WrapperPath string
	HomeDir     string
	IntervalSec int
	OutLogPath  string
	ErrLogPath  string
}

// WrapperData drives the markdown-sync wrapper script template render.
type WrapperData struct {
	BinaryPath      string
	DatabaseID      string
	ScanRoot        string
	KeychainService string
}

// ClaudeLogWrapperData drives the claude-log wrapper script template render.
// ClaudeProjectsRoot is optional: when empty the wrapper omits the env-var
// export and the binary falls back to the package default (~/.claude/projects).
type ClaudeLogWrapperData struct {
	BinaryPath         string
	DatabaseID         string
	ClaudeProjectsRoot string
	KeychainService    string
}

// Paths bundles the canonical filesystem locations for both launchd jobs.
// Markdown-sync fields (PlistPath, WrapperPath, OutLogPath, ErrLogPath) keep
// their original semantics so existing callers are byte-identical. Claude-log
// fields are namespaced with a ClaudeLog* prefix.
type Paths struct {
	HomeDir     string
	PlistPath   string
	WrapperPath string
	OutLogPath  string
	ErrLogPath  string
	LogDir      string
	WrapperDir  string

	ClaudeLogPlistPath   string
	ClaudeLogWrapperPath string
	ClaudeLogOutLogPath  string
	ClaudeLogErrLogPath  string
}

// DefaultPaths returns the conventional locations for both LaunchAgent files.
// The markdown-sync paths match the pre-claude-log layout byte-for-byte
// (~/bin/cortex-sync-wrapper.sh, ~/Library/Logs/cortex-sync.*.log) so
// reinstall does not perturb existing user installations. Claude-log files
// live next to them under the same directories with distinct basenames.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("user home dir: %w", err)
	}
	logDir := filepath.Join(home, "Library", "Logs")
	wrapperDir := filepath.Join(home, "bin")
	return Paths{
		HomeDir:     home,
		PlistPath:   filepath.Join(home, "Library", "LaunchAgents", MarkdownLabel+".plist"),
		WrapperPath: filepath.Join(wrapperDir, "cortex-sync-wrapper.sh"),
		OutLogPath:  filepath.Join(logDir, "cortex-sync.out.log"),
		ErrLogPath:  filepath.Join(logDir, "cortex-sync.err.log"),
		LogDir:      logDir,
		WrapperDir:  wrapperDir,

		ClaudeLogPlistPath:   filepath.Join(home, "Library", "LaunchAgents", ClaudeLogLabel+".plist"),
		ClaudeLogWrapperPath: filepath.Join(wrapperDir, "cortex-claudelog-wrapper.sh"),
		ClaudeLogOutLogPath:  filepath.Join(logDir, "cortex-claudelog.out.log"),
		ClaudeLogErrLogPath:  filepath.Join(logDir, "cortex-claudelog.err.log"),
	}, nil
}

// RenderPlist returns the rendered plist XML for the given data.
func RenderPlist(d PlistData) (string, error) {
	if d.IntervalSec <= 0 {
		// 900 = 15 minutes. Matches the original launchd default and the README.
		d.IntervalSec = 900
	}
	if d.Label == "" {
		d.Label = MarkdownLabel
	}
	tmpl, err := template.New("plist").Parse(plistTmplSrc)
	if err != nil {
		return "", fmt.Errorf("parse plist template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("execute plist template: %w", err)
	}
	return buf.String(), nil
}

// RenderWrapper returns the rendered markdown-sync wrapper shell script body.
func RenderWrapper(d WrapperData) (string, error) {
	if d.KeychainService == "" {
		d.KeychainService = "cortex-notion"
	}
	tmpl, err := template.New("wrapper").Parse(wrapperTmplSrc)
	if err != nil {
		return "", fmt.Errorf("parse wrapper template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("execute wrapper template: %w", err)
	}
	return buf.String(), nil
}

// RenderClaudeLogPlist returns the rendered claude-log plist XML. The plist
// template differs from the markdown-sync one only in the wrapper path and
// log paths the caller provides via PlistData.
func RenderClaudeLogPlist(d PlistData) (string, error) {
	if d.IntervalSec <= 0 {
		// 900 = 15 minutes; matches the markdown-sync cadence so both jobs
		// fire on similar schedules and the README only documents one value.
		d.IntervalSec = 900
	}
	if d.Label == "" {
		d.Label = ClaudeLogLabel
	}
	tmpl, err := template.New("claudelog-plist").Parse(claudeLogPlistTmplSrc)
	if err != nil {
		return "", fmt.Errorf("parse claude-log plist template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("execute claude-log plist template: %w", err)
	}
	return buf.String(), nil
}

// RenderClaudeLogWrapper returns the rendered claude-log wrapper shell script
// body. The wrapper exports the keychain token, the dedicated DB ID, and
// (optionally) the projects root before exec'ing `cortex-sync claude-log --all`.
func RenderClaudeLogWrapper(d ClaudeLogWrapperData) (string, error) {
	if d.KeychainService == "" {
		d.KeychainService = "cortex-notion"
	}
	tmpl, err := template.New("claudelog-wrapper").Parse(claudeLogWrapperTmplSrc)
	if err != nil {
		return "", fmt.Errorf("parse claude-log wrapper template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("execute claude-log wrapper template: %w", err)
	}
	return buf.String(), nil
}

// WriteFiles renders + writes the markdown-sync plist (0644) and wrapper
// script (0755), creating parent directories as needed.
func WriteFiles(paths Paths, plist PlistData, wrapper WrapperData) error {
	plistBody, err := RenderPlist(plist)
	if err != nil {
		return err
	}
	wrapperBody, err := RenderWrapper(wrapper)
	if err != nil {
		return err
	}
	if err := ensureSupportDirs(paths); err != nil {
		return err
	}
	if err := os.WriteFile(paths.PlistPath, []byte(plistBody), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	if err := os.WriteFile(paths.WrapperPath, []byte(wrapperBody), 0o755); err != nil {
		return fmt.Errorf("write wrapper: %w", err)
	}
	return nil
}

// WriteClaudeLogFiles renders + writes the claude-log plist (0644) and
// wrapper script (0755), creating parent directories as needed.
func WriteClaudeLogFiles(paths Paths, plist PlistData, wrapper ClaudeLogWrapperData) error {
	plistBody, err := RenderClaudeLogPlist(plist)
	if err != nil {
		return err
	}
	wrapperBody, err := RenderClaudeLogWrapper(wrapper)
	if err != nil {
		return err
	}
	if err := ensureSupportDirs(paths); err != nil {
		return err
	}
	if err := os.WriteFile(paths.ClaudeLogPlistPath, []byte(plistBody), 0o644); err != nil {
		return fmt.Errorf("write claude-log plist: %w", err)
	}
	if err := os.WriteFile(paths.ClaudeLogWrapperPath, []byte(wrapperBody), 0o755); err != nil {
		return fmt.Errorf("write claude-log wrapper: %w", err)
	}
	return nil
}

func ensureSupportDirs(paths Paths) error {
	if err := os.MkdirAll(filepath.Dir(paths.PlistPath), 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	if err := os.MkdirAll(paths.WrapperDir, 0o755); err != nil {
		return fmt.Errorf("mkdir wrapper dir: %w", err)
	}
	if err := os.MkdirAll(paths.LogDir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	return nil
}

// Bootstrap loads the markdown-sync LaunchAgent. If a previous job with the
// same label is already loaded, it is booted out first so this stays
// idempotent across re-installs (launchctl bootstrap will otherwise fail
// with exit 37/EEXIST).
func Bootstrap(ctx context.Context, paths Paths) error {
	return bootstrapLabel(ctx, paths.PlistPath, MarkdownLabel)
}

// BootstrapClaudeLog loads the claude-log LaunchAgent (idempotent — boots
// out any existing job with the same label first).
func BootstrapClaudeLog(ctx context.Context, paths Paths) error {
	return bootstrapLabel(ctx, paths.ClaudeLogPlistPath, ClaudeLogLabel)
}

func bootstrapLabel(ctx context.Context, plistPath, label string) error {
	uid := strconv.Itoa(os.Getuid())
	target := "gui/" + uid
	// best-effort bootout (ignore "no such job" errors)
	_ = run(ctx, "launchctl", "bootout", target, plistPath)
	if err := run(ctx, "launchctl", "bootstrap", target, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %w", label, err)
	}
	if err := run(ctx, "launchctl", "enable", target+"/"+label); err != nil {
		return fmt.Errorf("launchctl enable %s: %w", label, err)
	}
	return nil
}

// Bootout removes the markdown-sync LaunchAgent. Best-effort: returns nil if
// the job was already missing (launchctl exits non-zero with "Could not find
// specified service" in that case).
func Bootout(ctx context.Context, paths Paths) error {
	return bootoutPlist(ctx, paths.PlistPath)
}

// BootoutClaudeLog removes the claude-log LaunchAgent. Same best-effort
// semantics as Bootout.
func BootoutClaudeLog(ctx context.Context, paths Paths) error {
	return bootoutPlist(ctx, paths.ClaudeLogPlistPath)
}

func bootoutPlist(ctx context.Context, plistPath string) error {
	uid := strconv.Itoa(os.Getuid())
	target := "gui/" + uid
	err := run(ctx, "launchctl", "bootout", target, plistPath)
	if err == nil {
		return nil
	}
	// Not loaded is fine; surface other errors.
	msg := err.Error()
	if strings.Contains(msg, "Could not find") || strings.Contains(msg, "No such process") {
		return nil
	}
	return fmt.Errorf("launchctl bootout: %w", err)
}

// Kickstart triggers an immediate run of the loaded markdown-sync job (used
// for a smoke test after install).
func Kickstart(ctx context.Context) error {
	uid := strconv.Itoa(os.Getuid())
	return run(ctx, "launchctl", "kickstart", "-k", "gui/"+uid+"/"+MarkdownLabel)
}

// KickstartClaudeLog triggers an immediate run of the loaded claude-log job.
func KickstartClaudeLog(ctx context.Context) error {
	uid := strconv.Itoa(os.Getuid())
	return run(ctx, "launchctl", "kickstart", "-k", "gui/"+uid+"/"+ClaudeLogLabel)
}

// JobStatus is the parsed `launchctl print` output we care about for doctor.
type JobStatus struct {
	Loaded       bool
	LastExitCode string
	NextRunIn    string
	RawError     string
}

// Print runs `launchctl print` against the markdown-sync job and parses a few
// interesting fields. Any parse failure is non-fatal; the corresponding
// fields are simply left empty.
func Print(ctx context.Context) (JobStatus, error) {
	return printLabel(ctx, MarkdownLabel)
}

// PrintClaudeLog runs `launchctl print` against the claude-log job.
func PrintClaudeLog(ctx context.Context) (JobStatus, error) {
	return printLabel(ctx, ClaudeLogLabel)
}

func printLabel(ctx context.Context, label string) (JobStatus, error) {
	uid := strconv.Itoa(os.Getuid())
	cmd := exec.CommandContext(ctx, "launchctl", "print", "gui/"+uid+"/"+label)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return JobStatus{Loaded: false, RawError: strings.TrimSpace(stderr.String())}, nil
	}
	status := JobStatus{Loaded: true}
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "last exit code"):
			status.LastExitCode = strings.TrimSpace(strings.TrimPrefix(line, "last exit code ="))
		case strings.HasPrefix(line, "run interval"):
			status.NextRunIn = strings.TrimSpace(strings.TrimPrefix(line, "run interval ="))
		}
	}
	return status, nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		s := strings.TrimSpace(stderr.String())
		if s == "" {
			return err
		}
		return errors.New(s)
	}
	return nil
}
