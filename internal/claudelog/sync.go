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
type NotionGateway interface {
	VerifyDatabaseSchemaWith(ctx context.Context, dbID string, required []notion.PropDef) ([]notion.Issue, error)
	FindPageBySessionID(ctx context.Context, sessionID string) (string, error)
	CreatePageClaudeLog(ctx context.Context, dbID string, props notion.ClaudeLogProperties, blocks []map[string]any) (string, error)
}

// Runner is the claude-log subcommand's orchestration unit: parse JSONL,
// verify schema, dedupe by Session ID, create the page. Designed as a
// throw-away per invocation; no shared mutable state.
type Runner struct {
	gateway   NotionGateway
	dbID      string
	jsonlPath string
	logger    *slog.Logger
	dryRun    bool
	now       func() time.Time
}

// RunResult summarises what Run did so the CLI can print one-line output and
// tests can assert decisions without inspecting logs.
type RunResult struct {
	PageID       string
	Created      bool
	MessageCount int
	BlockCount   int
	Skipped      bool
}

// NewRunner constructs a Runner with the supplied gateway and metadata. The
// gateway may be a real *notion.Client (production) or a fake (tests). dbID
// is the claude-log DB ID; jsonlPath is the absolute path of the file to
// ingest.
func NewRunner(gateway NotionGateway, dbID, jsonlPath string, logger *slog.Logger, dryRun bool) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		gateway:   gateway,
		dbID:      dbID,
		jsonlPath: jsonlPath,
		logger:    logger,
		dryRun:    dryRun,
		now:       time.Now,
	}
}

// Run executes the full pipeline once: parse the JSONL, build properties,
// render markdown, convert to notion blocks, verify the DB schema, dedupe by
// Session ID, and (on first sync) POST /pages. Dry-run short-circuits before
// any network call.
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
	session, err := ParseSession(r.jsonlPath, r.logger)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", r.jsonlPath, err)
	}

	props := BuildProperties(session, r.jsonlPath, r.now())
	md := RenderMarkdown(session)
	blocks := markdown.Convert(md)

	res := &RunResult{
		MessageCount: len(session.Messages),
		BlockCount:   len(blocks),
	}

	if r.dryRun {
		r.logger.Info("claude-log dry-run plan",
			"session_id", session.SessionID,
			"project", session.Cwd,
			"messages", res.MessageCount,
			"blocks", res.BlockCount,
		)
		return res, nil
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
	if existing != "" {
		r.logger.Info("claude-log page already exists, skipping",
			"session_id", session.SessionID, "page_id", existing)
		res.PageID = existing
		res.Skipped = true
		return res, nil
	}

	pageID, err := r.gateway.CreatePageClaudeLog(ctx, r.dbID, props, blocks)
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}
	r.logger.Info("claude-log page created",
		"session_id", session.SessionID, "page_id", pageID, "blocks", res.BlockCount)
	res.PageID = pageID
	res.Created = true
	return res, nil
}

func formatIssues(issues []notion.Issue) string {
	parts := make([]string, 0, len(issues))
	for _, iss := range issues {
		parts = append(parts, iss.String())
	}
	return strings.Join(parts, "; ")
}
