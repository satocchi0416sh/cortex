package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/satocchi0416sh/cortex/internal/config"
	"github.com/satocchi0416sh/cortex/internal/launchd"
	"github.com/satocchi0416sh/cortex/internal/notion"
)

func runInstall(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync install", flag.ContinueOnError)
	intervalSec := fs.Int("interval", 900, "launchd StartInterval in seconds")
	dbIDFlag := fs.String("database-id", "", "override Notion DB ID (otherwise reuses existing wrapper or env)")
	scanRootFlag := fs.String("scan-root", "", "override scan root (otherwise reuses existing wrapper or ~/Projects)")
	claudeLogDBFlag := fs.String("claudelog-database-id", "", "claude-log Notion DB ID (otherwise reuses existing wrapper or env)")
	claudeProjectsRootFlag := fs.String("claude-projects-root", "", "claude projects root (otherwise reuses existing wrapper or ~/.claude/projects)")
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
	binaryPath, err := resolveBinaryPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ binary パス取得失敗:", err)
		return 1
	}

	// Determine DB ID and scan root: flags > existing wrapper > env > default.
	existing, _ := readExistingWrapper(paths.WrapperPath)
	dbID := firstNonEmpty(*dbIDFlag, existing.DatabaseID, os.Getenv("CORTEX_NOTION_DATABASE_ID"))
	scanRoot := firstNonEmpty(*scanRootFlag, existing.ScanRoot, os.Getenv("CORTEX_SCAN_ROOT"))
	if scanRoot == "" {
		scanRoot = config.DefaultScanRoot()
	}
	if dbID == "" {
		fmt.Fprintln(os.Stderr, "✗ DB ID 未指定: --database-id か CORTEX_NOTION_DATABASE_ID か既存 wrapper が必要です")
		return 2
	}
	// If user passed a URL via --database-id, normalize.
	if id, err := extractDatabaseID(dbID); err == nil {
		dbID = id
	}

	// Verify keychain entry exists; fail loudly if not (install is not init).
	if _, err := keychainGet(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ keychain entry が見つかりません。先に `cortex-sync init` を実行してください")
		return 1
	}

	// Soft-check markdown DB schema if reachable; non-fatal if Notion API unreachable.
	token, _ := keychainGet()
	if token != "" {
		client := newNotionClient(token, dbID, 2.5, slog.LevelError)
		if issues, err := client.VerifyDatabaseSchema(ctx, dbID); err == nil {
			missing := notion.MissingFromIssues(issues)
			if len(missing) > 0 {
				fmt.Fprintln(os.Stderr, "! schema に不足プロパティあり:")
				for _, m := range missing {
					fmt.Fprintf(os.Stderr, "    - %s (%s)\n", m.Name, m.Type)
				}
				fmt.Fprintln(os.Stderr, "  `cortex-sync init` で自動追加できます")
			}
		}
	}

	plistData, wrapperData := buildLaunchdData(paths, binaryPath, dbID, scanRoot, *intervalSec)
	if err := launchd.WriteFiles(paths, plistData, wrapperData); err != nil {
		fmt.Fprintln(os.Stderr, "✗ plist 配置失敗:", err)
		return 1
	}
	fmt.Println("✓ wrote", paths.PlistPath)
	fmt.Println("✓ wrote", paths.WrapperPath)

	if err := launchd.Bootstrap(ctx, paths); err != nil {
		fmt.Fprintln(os.Stderr, "✗ launchctl bootstrap 失敗:", err)
		return 1
	}
	fmt.Printf("✓ launchctl bootstrap %s (interval=%ds)\n", launchd.MarkdownLabel, *intervalSec)

	// claude-log job is opt-in: only install when a DB ID is reachable from
	// flags / existing wrapper / env. Silence (no error) if absent — markdown
	// sync alone is a valid configuration.
	existingClaude, _ := readExistingClaudeLogWrapper(paths.ClaudeLogWrapperPath)
	claudeDBID := firstNonEmpty(*claudeLogDBFlag, existingClaude.DatabaseID, os.Getenv("CORTEX_CLAUDELOG_DATABASE_ID"))
	if claudeDBID == "" {
		fmt.Println("• claude-log job: skipped (no DB ID configured)")
		return 0
	}
	if id, err := extractDatabaseID(claudeDBID); err == nil {
		claudeDBID = id
	}
	claudeRoot := firstNonEmpty(*claudeProjectsRootFlag, existingClaude.ClaudeProjectsRoot, os.Getenv("CORTEX_CLAUDE_PROJECTS_ROOT"))

	// Soft-check claude-log DB schema; non-fatal if unreachable.
	if token != "" {
		client := newNotionClient(token, claudeDBID, 2.5, slog.LevelError)
		if issues, err := client.VerifyDatabaseSchemaWith(ctx, claudeDBID, notion.ClaudeLogRequiredProperties); err == nil {
			missing := notion.MissingFromIssues(issues)
			if len(missing) > 0 {
				fmt.Fprintln(os.Stderr, "! claude-log schema に不足プロパティあり:")
				for _, m := range missing {
					fmt.Fprintf(os.Stderr, "    - %s (%s)\n", m.Name, m.Type)
				}
				fmt.Fprintln(os.Stderr, "  examples/notion-claudelog-schema.md を参照して手動追加してください")
			}
		}
	}

	clPlist, clWrapper := buildClaudeLogLaunchdData(paths, binaryPath, claudeDBID, claudeRoot, *intervalSec)
	if err := launchd.WriteClaudeLogFiles(paths, clPlist, clWrapper); err != nil {
		fmt.Fprintln(os.Stderr, "✗ claude-log plist 配置失敗:", err)
		return 1
	}
	fmt.Println("✓ wrote", paths.ClaudeLogPlistPath)
	fmt.Println("✓ wrote", paths.ClaudeLogWrapperPath)
	if err := launchd.BootstrapClaudeLog(ctx, paths); err != nil {
		fmt.Fprintln(os.Stderr, "✗ claude-log launchctl bootstrap 失敗:", err)
		return 1
	}
	fmt.Printf("✓ launchctl bootstrap %s (interval=%ds)\n", launchd.ClaudeLogLabel, *intervalSec)
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type wrapperVars struct {
	DatabaseID string
	ScanRoot   string
}

// readExistingWrapper grabs the prior CORTEX_NOTION_DATABASE_ID and
// CORTEX_SCAN_ROOT from a previously-installed wrapper script so reinstall
// doesn't require the user to re-supply them.
func readExistingWrapper(path string) (wrapperVars, error) {
	var v wrapperVars
	data, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if val, ok := captureExport(line, "CORTEX_NOTION_DATABASE_ID"); ok {
			v.DatabaseID = val
		}
		if val, ok := captureExport(line, "CORTEX_SCAN_ROOT"); ok {
			v.ScanRoot = val
		}
	}
	return v, nil
}

func captureExport(line, key string) (string, bool) {
	prefix := "export " + key + "="
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(line, prefix)
	rest = strings.Trim(rest, `"'`)
	rest = os.ExpandEnv(rest)
	return rest, true
}
