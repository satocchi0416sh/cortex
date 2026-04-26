# cortex-sync

`~/Projects/*/.serena/memories/*.md` を Notion データベースに **冪等に upsert 同期** する macOS 向け CLI。launchd で 15 分おきに起動する想定。

## 前提

- macOS
- Go 1.25+
- Notion workspace と integration token

## ビルド / インストール

```sh
go install github.com/satocchi0416sh/cortex/cmd/cortex-sync@latest
# あるいはこのリポジトリ内で:
go build -o cortex-sync ./cmd/cortex-sync
```

## Notion integration の作成

1. https://www.notion.so/profile/integrations を開く。
2. "New integration" → workspace を指定 → Internal integration として作成。
3. "Internal Integration Secret" をコピー（`CORTEX_NOTION_TOKEN` に設定）。
4. 同期先 database 側で右上の "..." → "Add connections" でこの integration を追加（必須）。

## Notion データベース

`examples/notion-database-schema.md` に詳述。次のプロパティを **完全一致の名前** で用意:

| プロパティ名 | 型 | 用途 |
|---|---|---|
| Name | Title | ファイル名（拡張子を除く） |
| External ID | Text | `sha256(project + "/" + relpath)` の hex |
| Project | Text | プロジェクト名 |
| File Path | Text | プロジェクトルートからの相対パス |
| Content Hash | Text | ファイル本文の sha256 |
| Last Synced | Date | 直近同期時刻 (ISO 8601 UTC) |

## 環境変数

| 変数名 | 既定値 | 必須 | 用途 |
|---|---|---|---|
| `CORTEX_NOTION_TOKEN` | — | yes | integration token |
| `CORTEX_NOTION_DATABASE_ID` | — | yes | 同期先 DB の UUID（ハイフン無しでも有り でも可） |
| `CORTEX_SCAN_ROOT` | `~/Projects` | no | 走査ルート |
| `CORTEX_STATE_FILE` | `~/.cortex/sync_state.json` | no | state ファイル |
| `CORTEX_GLOB_PATTERN` | `*/.serena/memories/*.md` | no | scan-root からの glob |
| `CORTEX_RPS` | `2.5` | no | Notion API 呼び出しの平均 RPS |
| `CORTEX_LOG_FORMAT` | `text` | no | `text` / `json` |

## CLI フラグ

```
--config <path>      yaml/json 設定ファイル
--dry-run            create/update/skip の判定だけ表示（API 呼び出しなし）
--verbose            slog レベルを Debug に
--once               1回実行して終了 (既定動作)
--no-jitter          起動直後の 0-30 秒ジッタを無効化
--scan-root <path>   走査ルートを上書き
--state-file <path>  state ファイルを上書き
```

## 動作確認

token 不要で plan を確認:

```sh
CORTEX_SCAN_ROOT=$HOME/Projects cortex-sync --dry-run
```

実同期（token を keychain から取得する例）:

```sh
export CORTEX_NOTION_TOKEN="$(security find-generic-password -s cortex-notion -w)"
export CORTEX_NOTION_DATABASE_ID="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
cortex-sync --once --verbose
```

## launchd への登録

1. バイナリを `$GOPATH/bin/cortex-sync` に配置（`go install ./cmd/cortex-sync`）。
2. `scripts/com.satoyoshi.cortex-sync.plist` を `~/Library/LaunchAgents/` にコピー。
3. ログディレクトリを作成: `mkdir -p ~/Library/Logs`。
4. 登録:

   ```sh
   launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.satoyoshi.cortex-sync.plist
   launchctl enable gui/$(id -u)/com.satoyoshi.cortex-sync
   launchctl kickstart -k gui/$(id -u)/com.satoyoshi.cortex-sync
   ```

5. 停止 / 解除:

   ```sh
   launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.satoyoshi.cortex-sync.plist
   ```

### token の渡し方

plist には secret を直書きしないでください。次のいずれかが推奨:

- launchd plist の `EnvironmentVariables` に `CORTEX_NOTION_TOKEN` を入れる（最も簡単だが平文）
- ラッパースクリプトを `ProgramArguments` に登録し、その中で `security find-generic-password -s cortex-notion -w` で keychain から取得して `exec env CORTEX_NOTION_TOKEN=... cortex-sync --once`

## ログ確認

```sh
tail -f ~/Library/Logs/cortex-sync.out.log
tail -f ~/Library/Logs/cortex-sync.err.log
```

`CORTEX_LOG_FORMAT=json` で構造化ログに切り替わります。

## トラブルシュート

- **429 が頻発する**: `CORTEX_RPS` を `2.0` などに下げる。Notion 上限は 3 RPS。
- **`object_not_found`**: integration を database に "Add connections" していない。
- **`property is not a valid X`**: README の表どおりにプロパティ名・型を一致させる（先頭大文字、半角スペース）。
- **state が壊れた / 全件再 upsert したい**: state ファイルを手動削除すると次回は **全件 create** 経路を通るが、Notion 側に重複ページが作られます。重複が嫌なら、削除後に integration からアクセス可能な既存ページを Notion 上で先に消すか、`External ID` で手動マージしてください。
- **`Notion-Version` 由来の 400**: 本ツールは `2026-03-11` 固定で送信。Notion の API 仕様変更で動かなくなった場合は `internal/notion/client.go` の `notionVersion` を更新。

## 設計メモ

- HTTP クライアントは `net/http` を直叩き、ペイロードは `map[string]any`。jomei/notionapi の Block 型は API バージョンが古いため不採用。
- 既存ページ更新は `replace_content` を使わず、`children` を 1件ずつ delete → 再 append。子ページ巻き添え trash 化を回避するため。
- レート制御は `golang.org/x/time/rate` の Limiter で全 API 呼び出し前に Wait。429 / 5xx は指数バックオフで最大 4 回リトライ。
- state は temp ファイル → rename で atomic write。
