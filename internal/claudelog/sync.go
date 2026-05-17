package claudelog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/satocchi0416sh/cortex/internal/markdown"
	"github.com/satocchi0416sh/cortex/internal/notion"
)

// NotionGateway is the slice of notion.Client the claude-log Runner depends
// on. Defining it as an interface keeps the runner unit-testable without
// hitting the network, and—because it explicitly excludes ReplaceChildren /
// DeleteBlock / ListChildIDs—makes destructive calls unrepresentable in the
// claude-log code path at the type-system level (not just by convention).
// The incremental sync path needs three extra methods (GetPageLastUUID,
// AppendNewChildren, UpdateClaudeLogCursorProperties), all of which are
// additive: none of them can delete or replace existing children, so a Notion
// page edited by the user between syncs is preserved by construction.
type NotionGateway interface {
	VerifyDatabaseSchemaWith(ctx context.Context, dbID string, required []notion.PropDef) ([]notion.Issue, error)
	FindPageBySessionID(ctx context.Context, sessionID string) (string, error)
	CreatePageClaudeLog(ctx context.Context, dbID string, props notion.ClaudeLogProperties, blocks []map[string]any) (string, error)
	GetPageLastUUID(ctx context.Context, pageID string) (string, error)
	AppendNewChildren(ctx context.Context, pageID string, blocks []map[string]any) error
	UpdateClaudeLogCursorProperties(ctx context.Context, pageID, lastUUID string, messageCount int, lastSynced time.Time) error
}

// ErrCursorStale is returned (wrapped) when the persisted Last UUID cursor no
// longer points at any message in the JSONL — typically because the JSONL was
// rewritten or the entry was deleted. The user must delete the Notion page and
// re-run from scratch; a rebuild flag is intentionally out of scope.
var ErrCursorStale = errors.New("cursor stale")

// Runner is the claude-log subcommand's orchestration unit: parse JSONL,
// verify schema, dedupe by Session ID, create or append. Designed as a
// throw-away per invocation; no shared mutable state beyond the state file.
type Runner struct {
	gateway   NotionGateway
	dbID      string
	jsonlPath string
	state     *State
	logger    *slog.Logger
	dryRun    bool
	now       func() time.Time
}

// RunResult summarises what Run did so the CLI can print one-line output and
// tests can assert decisions without inspecting logs. Created, Skipped, and
// Updated are mutually exclusive (at most one is true). For Updated runs,
// MessageCount and BlockCount count only the newly appended slice — not the
// total page size.
type RunResult struct {
	PageID       string
	Created      bool
	Updated      bool
	Skipped      bool
	MessageCount int
	BlockCount   int
}

// NewRunner constructs a Runner with the supplied gateway and metadata. The
// gateway may be a real *notion.Client (production) or a fake (tests). dbID
// is the claude-log DB ID; jsonlPath is the absolute path of the file to
// ingest. state is the per-session cursor store; nil disables persistence
// (e.g. dry-run, ad-hoc CLI invocations) and forces every existing-page sync
// to fall back to the Notion page property cursor.
func NewRunner(gateway NotionGateway, dbID, jsonlPath string, state *State, logger *slog.Logger, dryRun bool) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		gateway:   gateway,
		dbID:      dbID,
		jsonlPath: jsonlPath,
		state:     state,
		logger:    logger,
		dryRun:    dryRun,
		now:       time.Now,
	}
}

// Run executes the full pipeline once: parse the JSONL, build properties,
// render markdown, convert to notion blocks, verify the DB schema, and either
// create a new page (first sync) or append only the new messages since the
// last persisted cursor (subsequent syncs). Dry-run short-circuits before any
// network call.
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
	session, err := ParseSession(r.jsonlPath, r.logger)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", r.jsonlPath, err)
	}

	props := BuildProperties(session, r.jsonlPath, r.now())

	if r.dryRun {
		md := RenderMarkdown(session)
		blocks := markdown.Convert(md)
		r.logger.Info("claude-log dry-run plan",
			"session_id", session.SessionID,
			"project", session.Cwd,
			"messages", len(session.Messages),
			"blocks", len(blocks),
		)
		return &RunResult{
			MessageCount: len(session.Messages),
			BlockCount:   len(blocks),
		}, nil
	}

	if r.gateway == nil {
		return nil, errors.New("notion gateway is nil")
	}

	issues, err := r.gateway.VerifyDatabaseSchemaWith(ctx, r.dbID, notion.ClaudeLogRequiredProperties)
	if err != nil {
		return nil, fmt.Errorf("verify schema: %w", err)
	}
	if len(issues) > 0 {
		return nil, fmt.Errorf("claude-log DB schema is not compatible: %s", formatIssues(issues))
	}

	existing, err := r.gateway.FindPageBySessionID(ctx, session.SessionID)
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if existing == "" {
		return r.createPath(ctx, session, props)
	}
	return r.appendPath(ctx, session, props, existing)
}

// createPath is the first-sync branch: POST a new page with the entire
// rendered transcript and seed the cursor properties / state file from the
// final message.
func (r *Runner) createPath(ctx context.Context, session *Session, props notion.ClaudeLogProperties) (*RunResult, error) {
	md := RenderMarkdown(session)
	blocks := markdown.Convert(md)
	pageID, err := r.gateway.CreatePageClaudeLog(ctx, r.dbID, props, blocks)
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}
	syncedAt := r.now().UTC()
	if err := r.gateway.UpdateClaudeLogCursorProperties(ctx, pageID, props.LastUUID, props.MessageCount, syncedAt); err != nil {
		// Page creation already succeeded; failing here would leave the cursor
		// unset which next run will detect via empty Last UUID and treat as a
		// fresh-cursor path. Surface the error so the user can re-run.
		return nil, fmt.Errorf("seed cursor properties: %w", err)
	}
	if r.state != nil {
		r.state.Set(session.SessionID, SessionEntry{
			PageID:       pageID,
			LastUUID:     props.LastUUID,
			MessageCount: props.MessageCount,
			LastSynced:   syncedAt,
		})
		if err := r.state.Save(); err != nil {
			return nil, fmt.Errorf("save state: %w", err)
		}
	}
	r.logger.Info("claude-log page created",
		"session_id", session.SessionID, "page_id", pageID,
		"messages", props.MessageCount, "blocks", len(blocks))
	return &RunResult{
		PageID:       pageID,
		Created:      true,
		MessageCount: len(session.Messages),
		BlockCount:   len(blocks),
	}, nil
}

// appendPath is the incremental-sync branch: resolve the cursor (state file
// first, then the Notion page property), slice the session at cursor+1, and
// PATCH children + cursor properties.
func (r *Runner) appendPath(ctx context.Context, session *Session, props notion.ClaudeLogProperties, pageID string) (*RunResult, error) {
	cursorUUID := r.resolveCursor(ctx, session.SessionID, pageID)

	startIdx, err := r.locateCursorMessage(session, cursorUUID)
	if err != nil {
		return nil, err
	}

	newMessages := session.Messages[startIdx:]
	subSession := &Session{
		SessionID: session.SessionID,
		Cwd:       session.Cwd,
		StartedAt: session.StartedAt,
		Messages:  newMessages,
	}
	md := RenderMarkdown(subSession)
	blocks := markdown.Convert(md)
	syncedAt := r.now().UTC()

	if len(blocks) == 0 {
		// No new messages — just refresh Last Synced so the property reflects
		// the latest poll. props.LastUUID + props.MessageCount are the
		// already-current totals, so passing them is a no-op write that keeps
		// the row internally consistent.
		if err := r.gateway.UpdateClaudeLogCursorProperties(ctx, pageID, props.LastUUID, props.MessageCount, syncedAt); err != nil {
			return nil, fmt.Errorf("refresh last synced: %w", err)
		}
		if r.state != nil {
			r.state.Set(session.SessionID, SessionEntry{
				PageID:       pageID,
				LastUUID:     props.LastUUID,
				MessageCount: props.MessageCount,
				LastSynced:   syncedAt,
			})
			if err := r.state.Save(); err != nil {
				return nil, fmt.Errorf("save state: %w", err)
			}
		}
		r.logger.Info("claude-log page already up to date",
			"session_id", session.SessionID, "page_id", pageID)
		return &RunResult{PageID: pageID, Skipped: true, MessageCount: 0, BlockCount: 0}, nil
	}

	if err := r.gateway.AppendNewChildren(ctx, pageID, blocks); err != nil {
		return nil, fmt.Errorf("append new children: %w", err)
	}
	if err := r.gateway.UpdateClaudeLogCursorProperties(ctx, pageID, props.LastUUID, props.MessageCount, syncedAt); err != nil {
		return nil, fmt.Errorf("update cursor properties: %w", err)
	}
	if r.state != nil {
		r.state.Set(session.SessionID, SessionEntry{
			PageID:       pageID,
			LastUUID:     props.LastUUID,
			MessageCount: props.MessageCount,
			LastSynced:   syncedAt,
		})
		if err := r.state.Save(); err != nil {
			return nil, fmt.Errorf("save state: %w", err)
		}
	}
	r.logger.Info("claude-log appended new messages",
		"session_id", session.SessionID, "page_id", pageID,
		"new_messages", len(newMessages), "new_blocks", len(blocks))
	return &RunResult{
		PageID:       pageID,
		Updated:      true,
		MessageCount: len(newMessages),
		BlockCount:   len(blocks),
	}, nil
}

// resolveCursor reads the last-synced UUID from local state, falling back to
// the Notion page property when state is nil or has no entry for this
// session. A non-nil error from the property fetch is logged and treated as
// "no cursor known" so a transient Notion blip falls through to the
// stale-cursor branch (which produces a clear error) instead of silently
// re-uploading the whole transcript.
func (r *Runner) resolveCursor(ctx context.Context, sessionID, pageID string) string {
	if r.state != nil {
		if entry, ok := r.state.Get(sessionID); ok && entry.LastUUID != "" {
			return entry.LastUUID
		}
	}
	cursor, err := r.gateway.GetPageLastUUID(ctx, pageID)
	if err != nil {
		r.logger.Warn("failed to read Last UUID from Notion; cursor will be empty",
			"session_id", sessionID, "page_id", pageID, "err", err)
		return ""
	}
	return cursor
}

// locateCursorMessage returns the slice index pointing at the first message
// the caller should append. Returns 0 when cursor is empty (no cursor known →
// treat as full-resend, which the caller should detect as a stale page).
// Returns ErrCursorStale (wrapped) when cursor is non-empty but absent from
// the JSONL.
func (r *Runner) locateCursorMessage(session *Session, cursor string) (int, error) {
	if cursor == "" {
		// AC4 edge case: no cursor at all on an existing page means either
		// the page was created by an older cortex version or the state +
		// property are both empty. Surface this as stale rather than silently
		// re-uploading the whole transcript.
		return 0, fmt.Errorf("cursor missing: page exists but no Last UUID found (messages=%d); delete the Notion page and re-run, rebuild flag is out of scope: %w",
			len(session.Messages), ErrCursorStale)
	}
	for i, m := range session.Messages {
		if m.UUID == cursor {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("cursor stale: last synced uuid %s not found in jsonl (messages=%d); delete the Notion page and re-run, rebuild flag is out of scope: %w",
		cursor, len(session.Messages), ErrCursorStale)
}

func formatIssues(issues []notion.Issue) string {
	parts := make([]string, 0, len(issues))
	for _, iss := range issues {
		parts = append(parts, iss.String())
	}
	return strings.Join(parts, "; ")
}
