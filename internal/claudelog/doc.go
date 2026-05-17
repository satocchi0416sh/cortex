// Package claudelog parses Claude Code JSONL session files from
// ~/.claude/projects/<encoded-project>/<session-uuid>.jsonl and converts them
// into Notion pages via the existing notion package. MVP scope: create one
// page per session on first sync, dedupe by Session ID. Append / cursor /
// tool_use toggle rendering live in follow-up issues.
package claudelog
