package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/satocchi0416sh/cortex/internal/launchd"
	"github.com/satocchi0416sh/cortex/internal/notion"
)

// stderrLogger builds a text-format slog logger that writes to stderr at the
// given level. Used by short-lived subcommands (init, install, doctor) where
// a full config-driven logger is unnecessary.
func stderrLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// newNotionClient constructs a notion.Client with the standard subcommand
// options: stderr logger at the given level and a fixed RPS budget.
func newNotionClient(token, dbID string, rps float64, level slog.Level) *notion.Client {
	return notion.NewClient(notion.Options{
		Token:      token,
		DatabaseID: dbID,
		RPS:        rps,
		Logger:     stderrLogger(level),
	})
}

// buildLaunchdData populates the plist + wrapper template inputs that init and
// install both render. Centralized so the two subcommands cannot drift.
func buildLaunchdData(paths launchd.Paths, binaryPath, dbID, scanRoot string, intervalSec int) (launchd.PlistData, launchd.WrapperData) {
	return launchd.PlistData{
			Label:       launchd.MarkdownLabel,
			WrapperPath: paths.WrapperPath,
			HomeDir:     paths.HomeDir,
			IntervalSec: intervalSec,
			OutLogPath:  paths.OutLogPath,
			ErrLogPath:  paths.ErrLogPath,
		}, launchd.WrapperData{
			BinaryPath:      binaryPath,
			DatabaseID:      dbID,
			ScanRoot:        scanRoot,
			KeychainService: keychainService,
		}
}

// buildClaudeLogLaunchdData populates the claude-log plist + wrapper template
// inputs. Mirrors buildLaunchdData so init / install share a single source of
// truth for how the claude-log job is rendered.
func buildClaudeLogLaunchdData(paths launchd.Paths, binaryPath, dbID, projectsRoot string, intervalSec int) (launchd.PlistData, launchd.ClaudeLogWrapperData) {
	return launchd.PlistData{
			Label:       launchd.ClaudeLogLabel,
			WrapperPath: paths.ClaudeLogWrapperPath,
			HomeDir:     paths.HomeDir,
			IntervalSec: intervalSec,
			OutLogPath:  paths.ClaudeLogOutLogPath,
			ErrLogPath:  paths.ClaudeLogErrLogPath,
		}, launchd.ClaudeLogWrapperData{
			BinaryPath:         binaryPath,
			DatabaseID:         dbID,
			ClaudeProjectsRoot: projectsRoot,
			KeychainService:    keychainService,
		}
}

// readExistingClaudeLogWrapper grabs the prior CORTEX_CLAUDELOG_DATABASE_ID
// and CORTEX_CLAUDE_PROJECTS_ROOT from a previously-installed claude-log
// wrapper so a reinstall does not require re-supplying them. The wrapper file
// missing is not an error — callers treat that as "no prior install".
func readExistingClaudeLogWrapper(path string) (claudeLogWrapperVars, error) {
	var v claudeLogWrapperVars
	data, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if val, ok := captureExport(line, "CORTEX_CLAUDELOG_DATABASE_ID"); ok {
			v.DatabaseID = val
		}
		if val, ok := captureExport(line, "CORTEX_CLAUDE_PROJECTS_ROOT"); ok {
			v.ClaudeProjectsRoot = val
		}
	}
	return v, nil
}

type claudeLogWrapperVars struct {
	DatabaseID         string
	ClaudeProjectsRoot string
}
