// Package launchd provides idempotent install / uninstall helpers for the
// cortex-sync macOS launchd job (LaunchAgent). The plist + wrapper script are
// embedded as text/template at compile time so the binary is self-contained.
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

// Label is the launchd job label. Kept stable across versions so reinstall is
// idempotent (a previous bootstrap is bootout-able with the same label).
const Label = "com.satoyoshi.cortex-sync"

// PlistData drives the plist template render.
type PlistData struct {
	Label       string
	WrapperPath string
	HomeDir     string
	IntervalSec int
	OutLogPath  string
	ErrLogPath  string
}

// WrapperData drives the wrapper script template render.
type WrapperData struct {
	BinaryPath      string
	DatabaseID      string
	ScanRoot        string
	KeychainService string
}

// Paths bundles the canonical filesystem locations for the launchd job.
type Paths struct {
	HomeDir     string
	PlistPath   string
	WrapperPath string
	OutLogPath  string
	ErrLogPath  string
	LogDir      string
	WrapperDir  string
}

// DefaultPaths returns the conventional locations for the LaunchAgent files.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("user home dir: %w", err)
	}
	logDir := filepath.Join(home, "Library", "Logs")
	wrapperDir := filepath.Join(home, "bin")
	return Paths{
		HomeDir:     home,
		PlistPath:   filepath.Join(home, "Library", "LaunchAgents", Label+".plist"),
		WrapperPath: filepath.Join(wrapperDir, "cortex-sync-wrapper.sh"),
		OutLogPath:  filepath.Join(logDir, "cortex-sync.out.log"),
		ErrLogPath:  filepath.Join(logDir, "cortex-sync.err.log"),
		LogDir:      logDir,
		WrapperDir:  wrapperDir,
	}, nil
}

// RenderPlist returns the rendered plist XML for the given data.
func RenderPlist(d PlistData) (string, error) {
	if d.IntervalSec <= 0 {
		d.IntervalSec = 900
	}
	if d.Label == "" {
		d.Label = Label
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

// RenderWrapper returns the rendered wrapper shell script body.
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

// WriteFiles renders + writes plist (0644) and wrapper script (0755), creating
// parent directories as needed.
func WriteFiles(paths Paths, plist PlistData, wrapper WrapperData) error {
	plistBody, err := RenderPlist(plist)
	if err != nil {
		return err
	}
	wrapperBody, err := RenderWrapper(wrapper)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.PlistPath), 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	if err := os.MkdirAll(paths.WrapperDir, 0o755); err != nil {
		return fmt.Errorf("mkdir wrapper dir: %w", err)
	}
	if err := os.MkdirAll(paths.LogDir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	if err := os.WriteFile(paths.PlistPath, []byte(plistBody), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	if err := os.WriteFile(paths.WrapperPath, []byte(wrapperBody), 0o755); err != nil {
		return fmt.Errorf("write wrapper: %w", err)
	}
	return nil
}

// Bootstrap loads the LaunchAgent. If a previous job with the same label is
// already loaded, it is booted out first so this stays idempotent across
// re-installs (launchctl bootstrap will otherwise fail with exit 37/EEXIST).
func Bootstrap(ctx context.Context, paths Paths) error {
	uid := strconv.Itoa(os.Getuid())
	target := "gui/" + uid
	// best-effort bootout (ignore "no such job" errors)
	_ = run(ctx, "launchctl", "bootout", target, paths.PlistPath)
	if err := run(ctx, "launchctl", "bootstrap", target, paths.PlistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	if err := run(ctx, "launchctl", "enable", target+"/"+Label); err != nil {
		return fmt.Errorf("launchctl enable: %w", err)
	}
	return nil
}

// Bootout removes the LaunchAgent. Best-effort: returns nil if the job was
// already missing (launchctl exits non-zero with "Could not find specified
// service" in that case).
func Bootout(ctx context.Context, paths Paths) error {
	uid := strconv.Itoa(os.Getuid())
	target := "gui/" + uid
	err := run(ctx, "launchctl", "bootout", target, paths.PlistPath)
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

// Kickstart triggers an immediate run of the loaded job (used for a smoke test
// after install).
func Kickstart(ctx context.Context) error {
	uid := strconv.Itoa(os.Getuid())
	return run(ctx, "launchctl", "kickstart", "-k", "gui/"+uid+"/"+Label)
}

// JobStatus is the parsed `launchctl print` output we care about for doctor.
type JobStatus struct {
	Loaded       bool
	LastExitCode string
	NextRunIn    string
	RawError     string
}

// Print runs `launchctl print` and parses a few interesting fields. Any parse
// failure is non-fatal; we just leave the corresponding fields empty.
func Print(ctx context.Context) (JobStatus, error) {
	uid := strconv.Itoa(os.Getuid())
	cmd := exec.CommandContext(ctx, "launchctl", "print", "gui/"+uid+"/"+Label)
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
