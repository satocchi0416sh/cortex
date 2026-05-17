// Package claudelog parses Claude Code JSONL session files from
// ~/.claude/projects/<encoded-project>/<session-uuid>.jsonl and converts
// them into Notion pages via the existing notion package. Behaviours
// implemented: one page per session, dedupe by Session ID, incremental
// append by per-message UUID cursor, sidechain filtering, tool_use /
// tool_result toggle rendering, and ai-title aware Name fallback.
package claudelog
