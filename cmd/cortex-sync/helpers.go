package main

import (
	"log/slog"
	"os"

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
			Label:       launchd.Label,
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
