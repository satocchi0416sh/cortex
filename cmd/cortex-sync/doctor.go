package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/satocchi0416sh/cortex/internal/launchd"
	"github.com/satocchi0416sh/cortex/internal/notion"
	"github.com/satocchi0416sh/cortex/internal/scanner"
)

func runDoctor(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync doctor", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	fmt.Println("cortex-sync diagnostics")
	fmt.Println()

	paths, err := launchd.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}
	failures := 0
	check := func(ok bool, msg, hint string) {
		if ok {
			fmt.Println("✓", msg)
		} else {
			failures++
			if hint != "" {
				fmt.Printf("✗ %s — %s\n", msg, hint)
			} else {
				fmt.Println("✗", msg)
			}
		}
	}

	// 1. keychain
	token, err := keychainGet()
	switch {
	case errors.Is(err, errKeychainNotFound):
		check(false, "keychain entry ("+keychainService+") missing", "run `cortex-sync init`")
	case err != nil:
		check(false, "keychain entry: "+err.Error(), "")
	default:
		check(true, "keychain entry ("+keychainService+") found", "")
	}

	// 2. wrapper script + DB ID + scan root
	wrapper, err := readExistingWrapper(paths.WrapperPath)
	if err != nil {
		check(false, "wrapper script "+paths.WrapperPath+" missing", "run `cortex-sync install`")
	} else {
		check(true, "wrapper script "+paths.WrapperPath+" present", "")
	}

	// 3. Notion API + DB schema
	if token != "" && wrapper.DatabaseID != "" {
		client := newNotionClient(token, wrapper.DatabaseID, 5, slog.LevelError)
		info, err := client.GetDatabase(ctx, wrapper.DatabaseID)
		if err != nil {
			check(false, "Notion API / DB unreachable", err.Error())
		} else {
			check(true, fmt.Sprintf("DB %s accessible (integration connected)", short(info.ID)), "")
			issues, _ := client.VerifyDatabaseSchema(ctx, wrapper.DatabaseID)
			if len(issues) == 0 {
				check(true, "DB schema (6/6 properties present)", "")
			} else {
				missing := notion.MissingFromIssues(issues)
				if len(missing) > 0 {
					names := make([]string, 0, len(missing))
					for _, m := range missing {
						names = append(names, m.Name)
					}
					check(false, fmt.Sprintf("DB schema: missing %v", names), "run `cortex-sync init` to fix")
				}
				for _, iss := range issues {
					if iss.Kind == notion.IssueTypeMismatch {
						check(false, fmt.Sprintf("DB schema: %s type=%s want=%s", iss.Property, iss.Got, iss.Want),
							"manually fix in Notion UI")
					}
				}
			}
		}
	} else {
		check(false, "Notion DB schema check skipped", "missing token or DB ID")
	}

	// 4. plist
	if _, err := os.Stat(paths.PlistPath); err != nil {
		check(false, "plist missing at "+paths.PlistPath, "run `cortex-sync install`")
	} else {
		check(true, "plist installed at "+paths.PlistPath, "")
	}

	// 5. launchd job
	status, _ := launchd.Print(ctx)
	if status.Loaded {
		extra := ""
		if status.LastExitCode != "" {
			extra = ", last exit code = " + status.LastExitCode
		}
		check(true, "launchd job active"+extra, "")
	} else {
		hint := "run `cortex-sync install`"
		if status.RawError != "" {
			hint += " (" + status.RawError + ")"
		}
		check(false, "launchd job not loaded", hint)
	}

	// 6. scan root + .md count
	scanRoot := wrapper.ScanRoot
	if scanRoot == "" {
		scanRoot = defaultScanRoot()
	}
	if _, err := os.Stat(scanRoot); err != nil {
		check(false, "scan root "+scanRoot+" missing", "create it or set CORTEX_SCAN_ROOT")
	} else {
		files, _ := scanner.Scan(scanRoot, "*/.serena/memories/*.md")
		check(true, fmt.Sprintf("scan root %s exists (%d .md files)", scanRoot, len(files)), "")
	}

	fmt.Println()
	if failures == 0 {
		fmt.Println("All checks passed.")
		return 0
	}
	fmt.Printf("%d issue(s) found.\n", failures)
	return 1
}

func short(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "..."
}
