package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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

	// 1. bootout (best-effort)
	if err := launchd.Bootout(ctx, paths); err != nil {
		results = append(results, result{Step: "launchctl bootout", Note: "skipped (" + err.Error() + ")"})
	} else {
		results = append(results, result{Step: "launchctl bootout", Note: "ok"})
	}

	// 2. plist
	if err := os.Remove(paths.PlistPath); err != nil {
		if os.IsNotExist(err) {
			results = append(results, result{Step: "plist", Note: "(not present)"})
		} else {
			results = append(results, result{Step: "plist", Note: "error: " + err.Error()})
		}
	} else {
		results = append(results, result{Step: "plist", Note: "removed " + paths.PlistPath})
	}

	// 3. wrapper script
	if err := os.Remove(paths.WrapperPath); err != nil {
		if os.IsNotExist(err) {
			results = append(results, result{Step: "wrapper", Note: "(not present)"})
		} else {
			results = append(results, result{Step: "wrapper", Note: "error: " + err.Error()})
		}
	} else {
		results = append(results, result{Step: "wrapper", Note: "removed " + paths.WrapperPath})
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

	// 5. state file (confirm unless --delete-state or --yes)
	statePath := filepath.Join(paths.HomeDir, ".cortex", "sync_state.json")
	if _, err := os.Stat(statePath); err == nil {
		shouldDelete := *deleteState
		if !shouldDelete && !*yes {
			confirmed := false
			confirm := huh.NewConfirm().
				Title("state file " + statePath + " も削除しますか？").
				Description("削除すると次回 sync で全 page が create 経路（重複ページ生成リスク）").
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
		if shouldDelete {
			if err := os.Remove(statePath); err != nil {
				results = append(results, result{Step: "state", Note: "error: " + err.Error()})
			} else {
				results = append(results, result{Step: "state", Note: "removed " + statePath})
			}
		} else {
			results = append(results, result{Step: "state", Note: "kept " + statePath})
		}
	} else {
		results = append(results, result{Step: "state", Note: "(not present)"})
	}

	fmt.Println("cortex-sync uninstall summary:")
	for _, r := range results {
		fmt.Printf("  %-20s %s\n", r.Step, r.Note)
	}
	return 0
}
