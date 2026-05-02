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

	"github.com/charmbracelet/huh"
	"github.com/satocchi0416sh/cortex/internal/config"
	"github.com/satocchi0416sh/cortex/internal/launchd"
	"github.com/satocchi0416sh/cortex/internal/notion"
	"github.com/satocchi0416sh/cortex/internal/sync"
)

func runInit(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cortex-sync init", flag.ContinueOnError)
	intervalSec := fs.Int("interval", 900, "launchd StartInterval in seconds")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	fmt.Println("cortex-sync setup")
	fmt.Println()

	// [1/6] token
	fmt.Println("[1/6] Notion integration token を入力してください")
	fmt.Println("      → https://www.notion.so/profile/integrations で integration を作成し")
	fmt.Println("        \"Internal Integration Secret\" をコピーしてください")
	var token string
	tokenInput := huh.NewInput().
		Title("token").
		EchoMode(huh.EchoModePassword).
		Value(&token).
		Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return errors.New("token is required")
			}
			if !strings.HasPrefix(s, "secret_") && !strings.HasPrefix(s, "ntn_") {
				return errors.New("token should begin with secret_ or ntn_")
			}
			return nil
		})
	if err := tokenInput.Run(); err != nil {
		return huhExitCode(err)
	}
	token = strings.TrimSpace(token)
	if err := keychainSet("", token); err != nil {
		fmt.Fprintln(os.Stderr, "  ✗ keychain 保存失敗:", err)
		return 1
	}
	fmt.Println("  ✓ keychain 保存 (service=" + keychainService + ")")
	fmt.Println()

	// [2/6] DB URL or ID
	fmt.Println("[2/6] 同期先 Notion DB の URL または ID を入力してください")
	fmt.Println("      DB を開いて URL をコピーすればOK（v=... の前部分から DB ID 抽出）")
	fmt.Println("      事前に DB の \"•••\" → Connections で integration を Add してください")
	var dbInput string
	dbField := huh.NewInput().
		Title("DB URL or ID").
		Value(&dbInput).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("required")
			}
			if _, err := extractDatabaseID(s); err != nil {
				return err
			}
			return nil
		})
	if err := dbField.Run(); err != nil {
		return huhExitCode(err)
	}
	dbID, err := extractDatabaseID(dbInput)
	if err != nil {
		fmt.Fprintln(os.Stderr, "  ✗", err)
		return 1
	}
	fmt.Println("  ✓ DB ID =", dbID)
	fmt.Println()

	// [3/6] schema verify + auto-add missing
	fmt.Println("[3/6] DB スキーマを検証中...")
	client := newNotionClient(token, dbID, 2.5, slog.LevelWarn)
	info, err := client.GetDatabase(ctx, dbID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "  ✗ DB 取得失敗:", err)
		fmt.Fprintln(os.Stderr, "    integration を DB に Connections で Add したか確認してください")
		return 1
	}
	for _, want := range notion.RequiredProperties {
		got, ok := info.Properties[want.Name]
		switch {
		case !ok:
			fmt.Printf("  ✗ %s (missing) → 追加します\n", want.Name)
		case got.Type != want.Type:
			fmt.Printf("  ✗ %s (type=%s, want=%s)\n", want.Name, got.Type, want.Type)
		default:
			fmt.Printf("  ✓ %s (%s)\n", want.Name, want.Type)
		}
	}
	issues, err := client.VerifyDatabaseSchema(ctx, dbID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "  ✗ verify 失敗:", err)
		return 1
	}
	missing := notion.MissingFromIssues(issues)
	if len(missing) > 0 {
		if err := client.EnsureProperties(ctx, dbID, missing); err != nil {
			fmt.Fprintln(os.Stderr, "  ✗ プロパティ追加失敗:", err)
			return 1
		}
		fmt.Printf("  ✓ 不足プロパティ %d 件を追加完了\n", len(missing))
	}
	for _, iss := range issues {
		if iss.Kind == notion.IssueTypeMismatch {
			fmt.Fprintf(os.Stderr, "  ! 型ミスマッチ: %s (現状 %s, 期待 %s) — 手動修正してください\n",
				iss.Property, iss.Got, iss.Want)
		}
	}
	fmt.Println()

	// [4/6] scan root
	fmt.Println("[4/6] スキャンルートを設定")
	home, _ := os.UserHomeDir()
	defaultRoot := config.DefaultScanRoot()
	scanRoot := defaultRoot
	rootField := huh.NewInput().
		Title("Scan root").
		Description("Enter で既定 (~/Projects)").
		Placeholder(defaultRoot).
		Value(&scanRoot)
	if err := rootField.Run(); err != nil {
		return huhExitCode(err)
	}
	scanRoot = strings.TrimSpace(scanRoot)
	if scanRoot == "" {
		scanRoot = defaultRoot
	}
	scanRoot = expandHome(scanRoot)
	if _, err := os.Stat(scanRoot); err != nil {
		fmt.Fprintf(os.Stderr, "  ! scan root が存在しません: %s (作成しません)\n", scanRoot)
	}
	fmt.Println("  ✓ scan root =", scanRoot)
	fmt.Println()

	// [5/6] plist install
	fmt.Println("[5/6] launchd plist を配置")
	paths, err := launchd.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "  ✗", err)
		return 1
	}
	binaryPath, err := resolveBinaryPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "  ✗ binary パス取得失敗:", err)
		return 1
	}
	plistData, wrapperData := buildLaunchdData(paths, binaryPath, dbID, scanRoot, *intervalSec)
	if err := launchd.WriteFiles(paths, plistData, wrapperData); err != nil {
		fmt.Fprintln(os.Stderr, "  ✗ plist/wrapper 配置失敗:", err)
		return 1
	}
	fmt.Println("  ✓", paths.PlistPath)
	fmt.Println("  ✓ ラッパー", paths.WrapperPath)
	if err := launchd.Bootstrap(ctx, paths); err != nil {
		fmt.Fprintln(os.Stderr, "  ✗ launchctl bootstrap 失敗:", err)
		return 1
	}
	fmt.Printf("  ✓ launchctl bootstrap (%d 秒間隔で起動)\n", *intervalSec)
	fmt.Println()

	// [6/6] dry-run smoke test
	fmt.Println("[6/6] 初回同期確認 (dry-run)")
	cfg := &config.Config{
		NotionToken:      token,
		NotionDatabaseID: dbID,
		ScanRoot:         scanRoot,
		StateFile:        filepath.Join(home, ".cortex", "sync_state.json"),
		GlobPattern:      "*/.serena/memories/*.md",
		RPS:              2.5,
		LogFormat:        "text",
	}
	res, err := sync.New(cfg, stderrLogger(slog.LevelWarn), true).Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "  ✗ dry-run 失敗:", err)
		return 1
	}
	fmt.Printf("  ✓ %d files discovered (create=%d update=%d skip=%d)\n",
		res.Total, res.Created, res.Updated, res.Skipped)
	fmt.Println()

	fmt.Println("セットアップ完了。")
	fmt.Println("  status:    cortex-sync doctor")
	fmt.Println("  remove:    cortex-sync uninstall")
	fmt.Println("  ログ:      ", paths.ErrLogPath)
	return 0
}

// resolveBinaryPath returns the absolute path of the running cortex-sync.
// Used to bake into the wrapper script. If go install installed it under
// ~/go/bin we want that exact path so launchd uses the same binary.
func resolveBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return exe, nil
	}
	return abs, nil
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
