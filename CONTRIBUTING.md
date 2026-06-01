# Contributing to cortex-sync

Thanks for your interest in improving cortex-sync. This document covers how to
build, test, and submit changes.

## Prerequisites

- macOS (the launchd integration is macOS-only)
- Go 1.25+
- A Notion workspace and integration token if you want to exercise live syncs

## Getting started

```sh
git clone https://github.com/satocchi0416sh/cortex.git
cd cortex
go build ./...
```

## Build, vet, and test

Run these before opening a pull request:

```sh
go build ./...
go vet ./...
go test ./...
```

The Notion client and launchd helpers are covered by table-driven unit tests
under `internal/`. Tests must not call the live Notion API — use `--dry-run`
for manual end-to-end checks instead:

```sh
CORTEX_SCAN_ROOT="$HOME/Projects" go run ./cmd/cortex-sync --dry-run
```

## Commit and pull request conventions

- Use Conventional Commits for messages, e.g. `feat(launchd): ...`,
  `fix(claudelog): ...`, `docs: ...`. Scope is the affected package or area.
- Keep each pull request focused on a single concern.
- Reference the issue it closes in the PR description (e.g. `Closes #26`).
- Update the README when behavior, flags, or environment variables change.

## Project layout

| Path | Purpose |
|---|---|
| `cmd/cortex-sync` | CLI entry point and subcommands (`init`, `doctor`, `install`, `uninstall`, `claude-log`) |
| `internal/notion` | Notion API client (raw `net/http`, idempotent upsert) |
| `internal/launchd` | launchd plist / wrapper rendering and install helpers |
| `examples` | Notion database schema reference |

## Reporting issues

Open a GitHub issue describing the symptom, the command you ran, and the
relevant log output (`~/Library/Logs/cortex-sync.err.log`). Redact your Notion
token and database IDs.
