package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/satocchi0416sh/cortex/internal/launchd"
)

func runUninstall(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync uninstall", flag.ContinueOnError)
	deleteState := fs.Bool("delete-state", false, "also delete ~/.cortex/sync_state.json (skips confirmation)")
	yes := fs.Bool("yes", false, "assume yes for any prompts")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	paths, err := launchd.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}

	type result struct {
		Step string
		Note string
	}
	var results []result

	// 1. markdown-sync bootout (best-effort)
	if err := launchd.Bootout(ctx, paths); err != nil {
		results = append(results, result{Step: "launchctl bootout (markdown)", Note: "skipped (" + err.Error() + ")"})
	} else {
		results = append(results, result{Step: "launchctl bootout (markdown)", Note: "ok"})
	}

	// 1b. claude-log bootout (best-effort, may not exist)
	if err := launchd.BootoutClaudeLog(ctx, paths); err != nil {
		results = append(results, result{Step: "launchctl bootout (claude-log)", Note: "skipped (" + err.Error() + ")"})
	} else {
		results = append(results, result{Step: "launchctl bootout (claude-log)", Note: "ok"})
	}

	// 2. markdown plist
	if err := os.Remove(paths.PlistPath); err != nil {
		if os.IsNotExist(err) {
			results = append(results, result{Step: "plist (markdown)", Note: "(not present)"})
		} else {
			results = append(results, result{Step: "plist (markdown)", Note: "error: " + err.Error()})
		}
	} else {
		results = append(results, result{Step: "plist (markdown)", Note: "removed " + paths.PlistPath})
	}

	// 2b. claude-log plist
	if err := os.Remove(paths.ClaudeLogPlistPath); err != nil {
		if os.IsNotExist(err) {
			results = append(results, result{Step: "plist (claude-log)", Note: "(not present)"})
		} else {
			results = append(results, result{Step: "plist (claude-log)", Note: "error: " + err.Error()})
		}
	} else {
		results = append(results, result{Step: "plist (claude-log)", Note: "removed " + paths.ClaudeLogPlistPath})
	}

	// 3. markdown wrapper script
	if err := os.Remove(paths.WrapperPath); err != nil {
		if os.IsNotExist(err) {
			results = append(results, result{Step: "wrapper (markdown)", Note: "(not present)"})
		} else {
			results = append(results, result{Step: "wrapper (markdown)", Note: "error: " + err.Error()})
		}
	} else {
		results = append(results, result{Step: "wrapper (markdown)", Note: "removed " + paths.WrapperPath})
	}

	// 3b. claude-log wrapper script
	if err := os.Remove(paths.ClaudeLogWrapperPath); err != nil {
		if os.IsNotExist(err) {
			results = append(results, result{Step: "wrapper (claude-log)", Note: "(not present)"})
		} else {
			results = append(results, result{Step: "wrapper (claude-log)", Note: "error: " + err.Error()})
		}
	} else {
		results = append(results, result{Step: "wrapper (claude-log)", Note: "removed " + paths.ClaudeLogWrapperPath})
	}

	// 4. keychain entry
	if err := keychainDelete(); err != nil {
		if errors.Is(err, errKeychainNotFound) {
			results = append(results, result{Step: "keychain", Note: "(no entry)"})
		} else {
			results = append(results, result{Step: "keychain", Note: "error: " + err.Error()})
		}
	} else {
		results = append(results, result{Step: "keychain", Note: "removed (service=" + keychainService + ")"})
	}

	// 5. state files (confirm unless --delete-state or --yes). Both markdown
	// sync and claude-log use atomic local state files under ~/.cortex; a
	// single confirmation deletes whichever exist so the prompt count stays at
	// one regardless of which jobs the user had installed.
	statePaths := []string{
		filepath.Join(paths.HomeDir, ".cortex", "sync_state.json"),
		filepath.Join(paths.HomeDir, ".cortex", "claudelog_state.json"),
	}
	existingStates := make([]string, 0, len(statePaths))
	for _, sp := range statePaths {
		if _, err := os.Stat(sp); err == nil {
			existingStates = append(existingStates, sp)
		}
	}
	if len(existingStates) > 0 {
		shouldDelete := *deleteState
		if !shouldDelete && !*yes {
			confirmed := false
			confirm := huh.NewConfirm().
				Title("state file(s) も削除しますか？").
				Description("対象: " + strings.Join(existingStates, ", ") + "\n削除すると次回 sync で全 page が create 経路（重複ページ生成リスク）").
				Affirmative("Yes").
				Negative("No (default)").
				Value(&confirmed)
			if err := confirm.Run(); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					results = append(results, result{Step: "state", Note: "aborted; left in place"})
					confirmed = false
				} else {
					results = append(results, result{Step: "state", Note: "prompt error: " + err.Error()})
					confirmed = false
				}
			}
			shouldDelete = confirmed
		}
		for _, sp := range existingStates {
			if shouldDelete {
				if err := os.Remove(sp); err != nil {
					results = append(results, result{Step: "state " + filepath.Base(sp), Note: "error: " + err.Error()})
				} else {
					results = append(results, result{Step: "state " + filepath.Base(sp), Note: "removed " + sp})
				}
			} else {
				results = append(results, result{Step: "state " + filepath.Base(sp), Note: "kept " + sp})
			}
		}
	} else {
		results = append(results, result{Step: "state", Note: "(not present)"})
	}

	fmt.Println("cortex-sync uninstall summary:")
	for _, r := range results {
		fmt.Printf("  %-32s %s\n", r.Step, r.Note)
	}
	return 0
}
