package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/satocchi0416sh/cortex/internal/claudelog"
	"github.com/satocchi0416sh/cortex/internal/config"
	"github.com/satocchi0416sh/cortex/internal/notion"
)

// runClaudeLog handles `cortex-sync claude-log --session <uuid>`. It loads
// claude-log-specific config (separate DB id, projects root), locates the
// session JSONL on disk, then drives claudelog.Runner.
func runClaudeLog(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync claude-log", flag.ContinueOnError)
	var (
		sessionID  string
		dryRun     bool
		verbose    bool
		configPath string
		stateFile  string
	)
	fs.StringVar(&sessionID, "session", "", "session UUID (basename of the JSONL minus .jsonl)")
	fs.BoolVar(&dryRun, "dry-run", false, "parse and plan only, no Notion calls")
	fs.BoolVar(&verbose, "verbose", false, "debug logging")
	fs.StringVar(&configPath, "config", "", "path to yaml/json config file")
	fs.StringVar(&stateFile, "state-file", "", "override state file path (unused in MVP, reserved)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if sessionID == "" {
		fmt.Fprintln(os.Stderr, "claude-log: --session is required")
		fs.Usage()
		return 2
	}

	cfg, err := config.LoadForClaudeLog(config.Flags{
		ConfigPath: configPath,
		DryRun:     dryRun,
		Verbose:    verbose,
		StateFile:  stateFile,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 2
	}

	// Token: prefer explicit env/config, otherwise fall back to keychain.
	if !dryRun && cfg.NotionToken == "" {
		if token, kerr := keychainGet(); kerr == nil && token != "" {
			cfg.NotionToken = token
		}
	}
	if !dryRun && cfg.NotionToken == "" {
		fmt.Fprintln(os.Stderr, "claude-log: missing Notion token (set CORTEX_NOTION_TOKEN or run `cortex-sync init`)")
		return 1
	}

	logger := newLogger(cfg.LogFormat, verbose)
	slog.SetDefault(logger)

	if cfg.NotionDatabaseID != "" && cfg.NotionDatabaseID == cfg.ClaudeLogDatabaseID {
		logger.Warn("claude-log DB ID matches markdown sync DB ID; pages will share the database",
			"database_id", cfg.ClaudeLogDatabaseID)
	}

	jsonlPath, err := claudelog.LocateJSONL(cfg.ClaudeProjectsRoot, sessionID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "claude-log:", err)
		return 1
	}
	logger.Debug("located jsonl", "path", jsonlPath, "session", sessionID)

	var runner *claudelog.Runner
	if dryRun {
		runner = claudelog.NewRunner(nil, cfg.ClaudeLogDatabaseID, jsonlPath, logger, true)
	} else {
		client := notion.NewClient(notion.Options{
			Token:      cfg.NotionToken,
			DatabaseID: cfg.ClaudeLogDatabaseID,
			RPS:        cfg.RPS,
			Logger:     logger,
		})
		runner = claudelog.NewRunner(client, cfg.ClaudeLogDatabaseID, jsonlPath, logger, false)
	}

	res, err := runner.Run(ctx)
	if err != nil {
		logger.Error("claude-log failed", "err", err)
		return 1
	}

	switch {
	case dryRun:
		fmt.Printf("dry-run: would sync session %s (%d messages, %d blocks)\n",
			sessionID, res.MessageCount, res.BlockCount)
	case res.Skipped:
		fmt.Printf("skipped: page already exists for session %s (page_id=%s)\n", sessionID, res.PageID)
	case res.Created:
		fmt.Printf("created: page %s for session %s (%d messages, %d blocks)\n",
			res.PageID, sessionID, res.MessageCount, res.BlockCount)
	}
	return 0
}
