package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/satocchi0416sh/cortex/internal/launchd"
	"github.com/satocchi0416sh/cortex/internal/notion"
)

func runInstall(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync install", flag.ContinueOnError)
	intervalSec := fs.Int("interval", 900, "launchd StartInterval in seconds")
	dbIDFlag := fs.String("database-id", "", "override Notion DB ID (otherwise reuses existing wrapper or env)")
	scanRootFlag := fs.String("scan-root", "", "override scan root (otherwise reuses existing wrapper or ~/Projects)")
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
		home, _ := os.UserHomeDir()
		scanRoot = filepath.Join(home, "Projects")
	}
	if dbID == "" {
		fmt.Fprintln(os.Stderr, "✗ DB ID 未指定: --database-id か CORTEX_NOTION_DATABASE_ID か既存 wrapper が必要です")
		return 2
	}
	// If user passed a URL via --database-id, normalize.
	if id, perr := extractDatabaseID(dbID); perr == nil {
		dbID = id
	}

	// Verify keychain entry exists; fail loudly if not (install is not init).
	if _, err := keychainGet(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ keychain entry が見つかりません。先に `cortex-sync init` を実行してください")
		return 1
	}

	// Soft-check schema if reachable; non-fatal if Notion API unreachable.
	token, _ := keychainGet()
	if token != "" {
		client := notion.NewClient(notion.Options{
			Token:      token,
			DatabaseID: dbID,
			RPS:        2.5,
			Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		})
		if issues, verr := client.VerifyDatabaseSchema(ctx, dbID); verr == nil {
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

	plistData := launchd.PlistData{
		Label:       launchd.Label,
		WrapperPath: paths.WrapperPath,
		HomeDir:     paths.HomeDir,
		IntervalSec: *intervalSec,
		OutLogPath:  paths.OutLogPath,
		ErrLogPath:  paths.ErrLogPath,
	}
	wrapperData := launchd.WrapperData{
		BinaryPath:      binaryPath,
		DatabaseID:      dbID,
		ScanRoot:        scanRoot,
		KeychainService: keychainService,
	}
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
	fmt.Printf("✓ launchctl bootstrap (interval=%ds)\n", *intervalSec)
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
