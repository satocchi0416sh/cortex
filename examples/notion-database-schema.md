# Notion Database Schema

`cortex-sync` を実行する前に、Notion 上に以下のプロパティを持つデータベースを作成してください。プロパティ名は完全一致で必要です。

| プロパティ名 | 型 | 用途 |
|---|---|---|
| Name | Title | ファイル名（拡張子を除いた値） |
| External ID | Text (rich text) | 冪等 upsert のキー。`sha256(project + "/" + relpath)` を hex で格納 |
| Project | Text (rich text) | プロジェクトディレクトリ名 |
| File Path | Text (rich text) | プロジェクトルートからの相対パス |
| Content Hash | Text (rich text) | 同期したファイル本文の sha256（差分把握用） |
| Last Synced | Date | 直近の同期時刻 (ISO 8601, UTC) |

## 作成手順

1. Notion のサイドバーから新しい page を開き、"+ New" → "Table" を選び database を作成。
2. 既定の "Tags", "Files", などの不要なプロパティは削除して問題ありません。
3. 上表のプロパティを順に追加。型を間違えると同期時に 400 が返ります。
4. データベース右上の "..." → "Copy link" で URL を取得。`https://www.notion.so/<workspace>/<DATABASE_ID>?v=...` の **DATABASE_ID 部分** を `CORTEX_NOTION_DATABASE_ID` に設定します（ハイフン無しの 32 桁）。
5. database 右上の "..." → "Add connections" から、自分の integration を connection として追加。これを忘れると `object_not_found` エラーになります。
