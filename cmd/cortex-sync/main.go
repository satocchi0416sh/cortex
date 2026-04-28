package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/satocchi0416sh/cortex/internal/config"
	"github.com/satocchi0416sh/cortex/internal/sync"
)

const usage = `cortex-sync — Notion sync for ~/Projects/*/.serena/memories/*.md

Usage:
  cortex-sync [flags]              run a single sync (default)
  cortex-sync init                 interactive setup (token, DB, plist, launchd)
  cortex-sync doctor               diagnose token / DB schema / plist / launchd
  cortex-sync install              (re)install plist + launchd job (idempotent)
  cortex-sync uninstall            remove plist, launchd job, keychain entry

Sync flags:
  --config <path>      yaml/json config file
  --dry-run            plan only, no API calls
  --verbose            slog level=Debug
  --once               single run (default)
  --no-jitter          skip startup jitter sleep
  --scan-root <path>   override scan root
  --state-file <path>  override state file path

Run 'cortex-sync <subcommand> --help' for subcommand-specific flags.
`

func main() {
	args := os.Args[1:]
	sub, rest := splitSubcommand(args)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch sub {
	case "":
		os.Exit(runSync(ctx, rest))
	case "init":
		os.Exit(runInit(ctx, rest))
	case "doctor":
		os.Exit(runDoctor(ctx, rest))
	case "install":
		os.Exit(runInstall(ctx, rest))
	case "uninstall":
		os.Exit(runUninstall(ctx, rest))
	case "help", "-h", "--help":
		fmt.Print(usage)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", sub, usage)
		os.Exit(2)
	}
}

// splitSubcommand returns the leading non-flag arg as subcommand if it matches a
// known command, otherwise treats the entire arg list as flags for the default
// sync subcommand. This preserves backwards-compatibility for `cortex-sync --once`.
func splitSubcommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	first := args[0]
	switch first {
	case "init", "doctor", "install", "uninstall", "help", "-h", "--help":
		return first, args[1:]
	default:
		return "", args
	}
}

func runSync(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync", flag.ContinueOnError)
	var flags config.Flags
	fs.StringVar(&flags.ConfigPath, "config", "", "path to yaml/json config file")
	fs.BoolVar(&flags.DryRun, "dry-run", false, "discover + diff only, no API calls")
	fs.BoolVar(&flags.Verbose, "verbose", false, "debug logging")
	fs.BoolVar(&flags.Once, "once", false, "single run (default)")
	fs.BoolVar(&flags.NoJitter, "no-jitter", false, "skip startup jitter sleep")
	fs.StringVar(&flags.ScanRoot, "scan-root", "", "override scan root")
	fs.StringVar(&flags.StateFile, "state-file", "", "override state file path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	cfg, err := config.Load(flags)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 2
	}

	logger := newLogger(cfg.LogFormat, flags.Verbose)
	slog.SetDefault(logger)

	if !flags.NoJitter && !flags.DryRun {
		jitter := time.Duration(rand.Intn(31)) * time.Second
		logger.Debug("startup jitter", "sleep", jitter)
		time.Sleep(jitter)
	}

	runner := sync.New(cfg, logger, flags.DryRun)
	res, err := runner.Run(ctx)
	if err != nil {
		logger.Error("sync failed", "err", err)
		return 1
	}

	logger.Info("sync done",
		"total", res.Total,
		"created", res.Created,
		"updated", res.Updated,
		"skipped", res.Skipped,
		"failed", res.Failed,
		"dry_run", flags.DryRun,
	)
	if flags.DryRun {
		for _, p := range res.Plan {
			fmt.Printf("%-7s %s/%s\n", p.Action, p.Project, p.RelPath)
		}
	}
	if res.Failed > 0 && !flags.DryRun {
		return 1
	}
	return 0
}

func newLogger(format string, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// huhExitCode maps a huh form error to a process exit code. Ctrl-C / Esc on a
// huh prompt produces ErrUserAborted; we exit 130 (SIGINT convention) silently.
func huhExitCode(err error) int {
	if errors.Is(err, huh.ErrUserAborted) {
		return 130
	}
	return 1
}
