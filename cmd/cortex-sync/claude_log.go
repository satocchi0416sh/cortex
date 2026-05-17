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

// runClaudeLog handles `cortex-sync claude-log`. Two modes:
//
//   - `--session <uuid>`: locate and sync a single JSONL session (interactive
//     use, e.g. immediately after a Claude Code conversation).
//   - `--all`: scan ClaudeProjectsRoot for every `*.jsonl` and sync each one
//     in turn, best-effort (failures are logged but do not abort the loop).
//     Intended for the launchd job; exits non-zero only if at least one
//     session failed so launchd surfaces the issue via StandardErrorPath.
//
// `--session` and `--all` are mutually exclusive; specifying neither or both
// is a usage error (exit 2).
func runClaudeLog(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync claude-log", flag.ContinueOnError)
	var (
		sessionID  string
		all        bool
		dryRun     bool
		verbose    bool
		configPath string
		stateFile  string
	)
	fs.StringVar(&sessionID, "session", "", "session UUID (basename of the JSONL minus .jsonl)")
	fs.BoolVar(&all, "all", false, "scan ClaudeProjectsRoot and sync every *.jsonl (launchd mode)")
	fs.BoolVar(&dryRun, "dry-run", false, "parse and plan only, no Notion calls")
	fs.BoolVar(&verbose, "verbose", false, "debug logging")
	fs.StringVar(&configPath, "config", "", "path to yaml/json config file")
	fs.StringVar(&stateFile, "state-file", "", "override claude-log state file path (default ~/.cortex/claudelog_state.json)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	switch {
	case sessionID == "" && !all:
		fmt.Fprintln(os.Stderr, "claude-log: one of --session or --all is required")
		fs.Usage()
		return 2
	case sessionID != "" && all:
		fmt.Fprintln(os.Stderr, "claude-log: --session and --all are mutually exclusive")
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

	if all {
		return runClaudeLogAll(ctx, cfg, logger, dryRun)
	}
	return runClaudeLogSingle(ctx, cfg, logger, dryRun, sessionID)
}

// runClaudeLogSingle syncs exactly one session located by sessionID.
func runClaudeLogSingle(ctx context.Context, cfg *config.Config, logger *slog.Logger, dryRun bool, sessionID string) int {
	jsonlPath, err := claudelog.LocateJSONL(cfg.ClaudeProjectsRoot, sessionID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "claude-log:", err)
		return 1
	}
	logger.Debug("located jsonl", "path", jsonlPath, "session", sessionID)

	res, err := syncOneSession(ctx, cfg, logger, dryRun, jsonlPath)
	if err != nil {
		logger.Error("claude-log failed", "err", err)
		return 1
	}
	printSingleResult(sessionID, res, dryRun)
	return 0
}

// runClaudeLogAll is the launchd-driven mode: enumerate every session under
// ClaudeProjectsRoot and sync them best-effort. The state file is loaded once
// up front so all sessions share the same in-memory view; each session's
// successful sync triggers an atomic state Save (so a mid-run crash never
// loses progress for already-synced sessions). One failure does not abort the
// loop; the final exit code is 1 only if at least one session failed.
func runClaudeLogAll(ctx context.Context, cfg *config.Config, logger *slog.Logger, dryRun bool) int {
	paths, err := claudelog.LocateAllJSONLs(cfg.ClaudeProjectsRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "claude-log:", err)
		return 1
	}
	if len(paths) == 0 {
		fmt.Printf("claude-log --all: no jsonl found under %s\n", cfg.ClaudeProjectsRoot)
		return 0
	}

	var created, updated, skipped, failed int
	for _, jsonlPath := range paths {
		if err := ctx.Err(); err != nil {
			// Context cancelled (SIGINT/SIGTERM): stop iterating but report.
			logger.Warn("claude-log --all interrupted", "err", err)
			break
		}
		sessionID := claudelog.SessionIDFromPath(jsonlPath)
		res, syncErr := syncOneSession(ctx, cfg, logger, dryRun, jsonlPath)
		if syncErr != nil {
			failed++
			logger.Error("claude-log session failed",
				"session_id", sessionID, "path", jsonlPath, "err", syncErr)
			continue
		}
		switch {
		case res.Created:
			created++
		case res.Updated:
			updated++
		case res.Skipped:
			skipped++
		}
		logger.Debug("claude-log session done",
			"session_id", sessionID, "created", res.Created,
			"updated", res.Updated, "skipped", res.Skipped)
	}

	fmt.Printf("claude-log --all: total=%d created=%d updated=%d skipped=%d failed=%d\n",
		len(paths), created, updated, skipped, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// syncOneSession builds a Runner for one jsonl path and runs it. State is
// loaded fresh each call so callers can stick to a simple sequential loop; the
// state file's atomic-write Save means concurrent callers would still be
// race-free, though today nothing does that.
func syncOneSession(ctx context.Context, cfg *config.Config, logger *slog.Logger, dryRun bool, jsonlPath string) (*claudelog.RunResult, error) {
	var runner *claudelog.Runner
	if dryRun {
		runner = claudelog.NewRunner(nil, cfg.ClaudeLogDatabaseID, jsonlPath, nil, logger, true)
	} else {
		state, err := claudelog.LoadState(cfg.StateFile)
		if err != nil {
			return nil, fmt.Errorf("load state: %w", err)
		}
		client := notion.NewClient(notion.Options{
			Token:      cfg.NotionToken,
			DatabaseID: cfg.ClaudeLogDatabaseID,
			RPS:        cfg.RPS,
			Logger:     logger,
		})
		runner = claudelog.NewRunner(client, cfg.ClaudeLogDatabaseID, jsonlPath, state, logger, false)
	}
	return runner.Run(ctx)
}

// printSingleResult emits the one-line summary used by the --session path so
// the interactive UX from before --all stays byte-identical.
func printSingleResult(sessionID string, res *claudelog.RunResult, dryRun bool) {
	switch {
	case dryRun:
		fmt.Printf("dry-run: would sync session %s (%d messages, %d blocks)\n",
			sessionID, res.MessageCount, res.BlockCount)
	case res.Updated:
		fmt.Printf("appended: %d new messages to page %s (session %s, %d new blocks)\n",
			res.MessageCount, res.PageID, sessionID, res.BlockCount)
	case res.Skipped:
		fmt.Printf("skipped: page already up to date (session %s, page_id=%s)\n", sessionID, res.PageID)
	case res.Created:
		fmt.Printf("created: page %s for session %s (%d messages, %d blocks)\n",
			res.PageID, sessionID, res.MessageCount, res.BlockCount)
	}
}
