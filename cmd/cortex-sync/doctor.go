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

	// 5. launchd job (markdown)
	status, _ := launchd.Print(ctx)
	if status.Loaded {
		extra := ""
		if status.LastExitCode != "" {
			extra = ", last exit code = " + status.LastExitCode
		}
		check(true, "launchd job "+launchd.MarkdownLabel+" active"+extra, "")
	} else {
		hint := "run `cortex-sync install`"
		if status.RawError != "" {
			hint += " (" + status.RawError + ")"
		}
		check(false, "launchd job "+launchd.MarkdownLabel+" not loaded", hint)
	}

	// 6. scan root + .md count
	scanRoot := wrapper.ScanRoot
	if scanRoot == "" {
		scanRoot = config.DefaultScanRoot()
	}
	if _, err := os.Stat(scanRoot); err != nil {
		check(false, "scan root "+scanRoot+" missing", "create it or set CORTEX_SCAN_ROOT")
	} else {
		files, _ := scanner.Scan(scanRoot, "*/.serena/memories/*.md")
		check(true, fmt.Sprintf("scan root %s exists (%d .md files)", scanRoot, len(files)), "")
	}

	// 7. claude-log wrapper, DB schema, plist, launchd job, projects root.
	// All of these are opt-in: if the user never set up the claude-log job,
	// these checks report status without counting as failures.
	fmt.Println()
	fmt.Println("--- claude-log ---")
	claudeWrapper, claudeWrapperErr := readExistingClaudeLogWrapper(paths.ClaudeLogWrapperPath)
	claudeDBID := claudeWrapper.DatabaseID
	if claudeDBID == "" {
		claudeDBID = os.Getenv("CORTEX_CLAUDELOG_DATABASE_ID")
	}
	switch {
	case claudeWrapperErr == nil:
		check(true, "claude-log wrapper "+paths.ClaudeLogWrapperPath+" present", "")
	case claudeDBID != "":
		check(false, "claude-log wrapper "+paths.ClaudeLogWrapperPath+" missing", "run `cortex-sync install`")
	default:
		// No wrapper, no env-supplied DB ID, no configuration: report as
		// "not configured" rather than as a failure.
		fmt.Println("• claude-log: not configured (skip)")
	}

	if claudeDBID != "" {
		if token != "" {
			client := newNotionClient(token, claudeDBID, 5, slog.LevelError)
			info, err := client.GetDatabase(ctx, claudeDBID)
			if err != nil {
				check(false, "claude-log Notion API / DB unreachable", err.Error())
			} else {
				check(true, fmt.Sprintf("claude-log DB %s accessible", short(info.ID)), "")
				issues, _ := client.VerifyDatabaseSchemaWith(ctx, claudeDBID, notion.ClaudeLogRequiredProperties)
				if len(issues) == 0 {
					check(true, fmt.Sprintf("claude-log DB schema (%d/%d properties present)",
						len(notion.ClaudeLogRequiredProperties), len(notion.ClaudeLogRequiredProperties)), "")
				} else {
					missing := notion.MissingFromIssues(issues)
					if len(missing) > 0 {
						names := make([]string, 0, len(missing))
						for _, m := range missing {
							names = append(names, m.Name)
						}
						check(false, fmt.Sprintf("claude-log DB schema: missing %v", names),
							"add properties via Notion UI; see examples/")
					}
					for _, iss := range issues {
						if iss.Kind == notion.IssueTypeMismatch {
							check(false, fmt.Sprintf("claude-log DB schema: %s type=%s want=%s",
								iss.Property, iss.Got, iss.Want), "manually fix in Notion UI")
						}
					}
				}
			}
		}

		if _, err := os.Stat(paths.ClaudeLogPlistPath); err != nil {
			check(false, "claude-log plist missing at "+paths.ClaudeLogPlistPath, "run `cortex-sync install`")
		} else {
			check(true, "claude-log plist installed at "+paths.ClaudeLogPlistPath, "")
		}

		claudeStatus, _ := launchd.PrintClaudeLog(ctx)
		if claudeStatus.Loaded {
			extra := ""
			if claudeStatus.LastExitCode != "" {
				extra = ", last exit code = " + claudeStatus.LastExitCode
			}
			check(true, "launchd job "+launchd.ClaudeLogLabel+" active"+extra, "")
		} else {
			hint := "run `cortex-sync install`"
			if claudeStatus.RawError != "" {
				hint += " (" + claudeStatus.RawError + ")"
			}
			check(false, "launchd job "+launchd.ClaudeLogLabel+" not loaded", hint)
		}

		claudeRoot := claudeWrapper.ClaudeProjectsRoot
		if claudeRoot == "" {
			claudeRoot = os.Getenv("CORTEX_CLAUDE_PROJECTS_ROOT")
		}
		if claudeRoot == "" {
			claudeRoot = config.DefaultClaudeProjectsRoot()
		}
		if _, err := os.Stat(claudeRoot); err != nil {
			// Not a hard failure: first run after install may legitimately have
			// no projects yet.
			fmt.Println("• claude projects root", claudeRoot, "missing (no sessions yet)")
		} else {
			files, _ := claudelog.LocateAllJSONLs(claudeRoot)
			check(true, fmt.Sprintf("claude projects root %s exists (%d .jsonl files)", claudeRoot, len(files)), "")
		}
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
