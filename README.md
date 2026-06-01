# cortex-sync

[![CI](https://github.com/satocchi0416sh/cortex/actions/workflows/ci.yml/badge.svg)](https://github.com/satocchi0416sh/cortex/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/satocchi0416sh/cortex.svg)](https://pkg.go.dev/github.com/satocchi0416sh/cortex)
[![Go Report Card](https://goreportcard.com/badge/github.com/satocchi0416sh/cortex)](https://goreportcard.com/report/github.com/satocchi0416sh/cortex)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Latest release](https://img.shields.io/github/v/release/satocchi0416sh/cortex?sort=semver)](https://github.com/satocchi0416sh/cortex/releases)
[![Platform: macOS](https://img.shields.io/badge/platform-macOS-lightgrey.svg)](#prerequisites)

A macOS CLI that **idempotently upserts** `~/Projects/*/.serena/memories/*.md`
into a Notion database. It is designed to run every 15 minutes under launchd.

An optional **claude-log** mode syncs `~/.claude/projects/**/*.jsonl` (Claude
Code conversation history) into a separate Notion database. Each mode is
registered as its own independent launchd job.

## Quick Start

```sh
# 1. Install
go install github.com/satocchi0416sh/cortex/cmd/cortex-sync@latest

# 2. Interactive setup
#    (token / DB / schema validation / plist placement / launchd registration /
#     first-run check / optional claude-log setup)
cortex-sync init
```

Before running `init`, create a [Notion integration](https://www.notion.so/profile/integrations)
and add it to the target database via "•••" → Connections. For claude-log,
prepare a second database and add the same integration to it.

Day-to-day operations after setup:

```sh
cortex-sync doctor       # Diagnose state (token / both DB schemas / both plists / both launchd jobs)
cortex-sync install      # Re-place both plists + re-register both launchd jobs
cortex-sync uninstall    # Full uninstall (markdown + claude-log plists / jobs / wrappers)
```

## Prerequisites

- macOS
- Go 1.25+
- A Notion workspace and integration token

## Installation

```sh
go install github.com/satocchi0416sh/cortex/cmd/cortex-sync@latest
# or, from inside this repository:
go build -o cortex-sync ./cmd/cortex-sync
```

## Creating a Notion integration

1. Open https://www.notion.so/profile/integrations.
2. "New integration" → pick a workspace → create it as an Internal integration.
3. Copy the "Internal Integration Secret" (set it as `CORTEX_NOTION_TOKEN`).
4. On the target database, click "..." → "Add connections" and add this
   integration (required).

## Notion database schema

Full details live in `examples/notion-database-schema.md`. Create the following
properties with **exactly matching names**:

| Property | Type | Purpose |
|---|---|---|
| Name | Title | File name (without extension) |
| External ID | Text | Hex of `sha256(project + "/" + relpath)` |
| Project | Text | Project name |
| File Path | Text | Path relative to the project root |
| Content Hash | Text | `sha256` of the file body |
| Last Synced | Date | Last sync time (ISO 8601 UTC) |

## Environment variables

| Variable | Default | Required | Purpose |
|---|---|---|---|
| `CORTEX_NOTION_TOKEN` | — | yes | Integration token |
| `CORTEX_NOTION_DATABASE_ID` | — | yes | Target DB UUID (with or without hyphens) |
| `CORTEX_SCAN_ROOT` | `~/Projects` | no | Scan root |
| `CORTEX_STATE_FILE` | `~/.cortex/sync_state.json` | no | State file |
| `CORTEX_GLOB_PATTERN` | `*/.serena/memories/*.md` | no | Glob relative to scan root |
| `CORTEX_RPS` | `2.5` | no | Average requests per second to the Notion API |
| `CORTEX_LOG_FORMAT` | `text` | no | `text` / `json` |
| `CORTEX_CLAUDELOG_DATABASE_ID` | — | yes for claude-log | Target DB UUID for Claude Code history |
| `CORTEX_CLAUDE_PROJECTS_ROOT` | `~/.claude/projects` | no | Root of Claude Code session JSONL files |

## CLI

```
cortex-sync [flags]                            One-shot sync (default)
cortex-sync init                               Interactive setup (confirms claude-log setup mid-flow)
cortex-sync doctor                             Diagnose state (markdown + claude-log)
cortex-sync install                            Idempotently re-place plists + launchd jobs
cortex-sync uninstall                          Full uninstall (both jobs)
cortex-sync claude-log --session <uuid>        Sync a single session to Notion (interactive)
cortex-sync claude-log --all                   Best-effort sync of every *.jsonl under the projects root (for launchd)
```

`claude-log` flags:

```
--session <uuid>     Session ID derived from the file name (<uuid>.jsonl)
--all                Sync every *.jsonl under CORTEX_CLAUDE_PROJECTS_ROOT in sequence
--dry-run            Print the plan without calling the Notion API
--verbose            Set the slog level to Debug
--config <path>      yaml/json config file
--state-file <path>  Override the claude-log state file (default ~/.cortex/claudelog_state.json)
```

`--session` and `--all` are mutually exclusive. Passing both or neither exits 2.

Sync flags:

```
--config <path>      yaml/json config file
--dry-run            Print create/update/skip decisions only (no API calls)
--verbose            Set the slog level to Debug
--once               Run once and exit (default)
--no-jitter          Disable the 0–30s startup jitter
--scan-root <path>   Override the scan root
--state-file <path>  Override the state file
```

## Dry run

Verify the plan without a token:

```sh
CORTEX_SCAN_ROOT="$HOME/Projects" cortex-sync --dry-run
```

Real sync (token fetched from the keychain):

```sh
export CORTEX_NOTION_TOKEN="$(security find-generic-password -s cortex-notion -w)"
export CORTEX_NOTION_DATABASE_ID="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
cortex-sync --once --verbose
```

## launchd: a two-job layout

`cortex-sync init` (and `install`) register two launchd LaunchAgents:

| Label | Wrapper | Command | Synced source |
|---|---|---|---|
| `com.cortex.sync` | `~/bin/cortex-sync-wrapper.sh` | `cortex-sync --once` | `~/Projects/*/.serena/memories/*.md` |
| `com.cortex.claudelog` | `~/bin/cortex-claudelog-wrapper.sh` | `cortex-sync claude-log --all` | `~/.claude/projects/**/*.jsonl` |

The binary embeds the plist and wrapper templates and writes them to
`~/Library/LaunchAgents/com.cortex.sync.plist` and
`~/Library/LaunchAgents/com.cortex.claudelog.plist` at install time — there is
no checked-in plist to copy by hand.

claude-log is **opt-in**: answer No at the `cortex-sync init` confirmation, or
leave `CORTEX_CLAUDELOG_DATABASE_ID` unset, and its plist is not placed. The
markdown sync works on its own.

Check both jobs with:

```sh
launchctl list | grep cortex
```

### Passing the token

Never hard-code the secret into the plist. Either:

- Put `CORTEX_NOTION_TOKEN` in the plist `EnvironmentVariables` (simplest, but
  plaintext), or
- Let the wrapper script fetch it from the keychain at runtime with
  `security find-generic-password -s cortex-notion -w` and `exec` the binary
  with the value in the environment (this is what `init` sets up).

### Migrating from older versions

Older releases registered the two jobs under a different launchd label prefix.
The labels are now `com.cortex.sync` and `com.cortex.claudelog`. After
upgrading, remove any legacy jobs once so they do not run alongside the new
ones. List what is currently loaded:

```sh
launchctl list | grep cortex
```

For each job **not** named `com.cortex.sync` or `com.cortex.claudelog`, bootout
it, delete its plist, then register the new jobs:

```sh
launchctl bootout gui/$(id -u)/<old-label>
rm -f ~/Library/LaunchAgents/<old-label>.plist
cortex-sync install   # register the new com.cortex.* jobs
```

## Logs

```sh
tail -f ~/Library/Logs/cortex-sync.out.log
tail -f ~/Library/Logs/cortex-sync.err.log
```

Set `CORTEX_LOG_FORMAT=json` for structured logs.

## Troubleshooting

- **Frequent 429s**: lower `CORTEX_RPS` (e.g. to `2.0`). The Notion limit is 3 RPS.
- **`object_not_found`**: the integration was not added to the database via
  "Add connections".
- **`property is not a valid X`**: match the property names and types exactly as
  in the schema table (capitalization, ASCII spaces).
- **Corrupt state / want to re-upsert everything**: deleting the state file
  routes the next run through the **create** path, which creates duplicate pages
  in Notion. To avoid duplicates, delete the existing integration-visible pages
  in Notion first, or merge manually by `External ID`.
- **400 from `Notion-Version`**: this tool pins `2026-03-11`. If a Notion API
  change breaks it, update `notionVersion` in `internal/notion/client.go`.

## Design notes

- The HTTP client calls `net/http` directly with `map[string]any` payloads.
  jomei/notionapi's Block types target an older API version and are not used.
- Existing pages are updated by deleting and re-appending `children` one at a
  time rather than via `replace_content`, to avoid trashing nested child pages.
- Rate limiting uses a `golang.org/x/time/rate` limiter that `Wait`s before
  every API call. 429 / 5xx responses are retried with exponential backoff up to
  four times.
- State is written atomically via a temp file followed by a rename.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build, test, and pull request
guidelines.

## License

[MIT](LICENSE) © 2026 cortex contributors
