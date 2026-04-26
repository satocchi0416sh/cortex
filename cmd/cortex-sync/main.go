package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/satocchi0416sh/cortex/internal/config"
	"github.com/satocchi0416sh/cortex/internal/sync"
)

func main() {
	var flags config.Flags
	flag.StringVar(&flags.ConfigPath, "config", "", "path to yaml/json config file")
	flag.BoolVar(&flags.DryRun, "dry-run", false, "discover + diff only, no API calls")
	flag.BoolVar(&flags.Verbose, "verbose", false, "debug logging")
	flag.BoolVar(&flags.Once, "once", false, "single run (default)")
	flag.BoolVar(&flags.NoJitter, "no-jitter", false, "skip startup jitter sleep")
	flag.StringVar(&flags.ScanRoot, "scan-root", "", "override scan root")
	flag.StringVar(&flags.StateFile, "state-file", "", "override state file path")
	flag.Parse()

	cfg, err := config.Load(flags)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}

	logger := newLogger(cfg.LogFormat, flags.Verbose)
	slog.SetDefault(logger)

	if !flags.NoJitter && !flags.DryRun {
		jitter := time.Duration(rand.Intn(31)) * time.Second
		logger.Debug("startup jitter", "sleep", jitter)
		time.Sleep(jitter)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	runner := sync.New(cfg, logger, flags.DryRun)
	res, err := runner.Run(ctx)
	if err != nil {
		logger.Error("sync failed", "err", err)
		os.Exit(1)
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
		os.Exit(1)
	}
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
