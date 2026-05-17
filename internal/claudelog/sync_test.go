package claudelog

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/satocchi0416sh/cortex/internal/notion"
)

type fakeGateway struct {
	verifyIssues []notion.Issue
	verifyErr    error
	findID       string
	findErr      error
	createID     string
	createErr    error

	verifyCalls int
	findCalls   int
	createCalls int
	lastDBID    string
	lastProps   notion.ClaudeLogProperties
}

func (f *fakeGateway) VerifyDatabaseSchemaWith(_ context.Context, dbID string, _ []notion.PropDef) ([]notion.Issue, error) {
	f.verifyCalls++
	f.lastDBID = dbID
	return f.verifyIssues, f.verifyErr
}

func (f *fakeGateway) FindPageBySessionID(_ context.Context, _ string) (string, error) {
	f.findCalls++
	return f.findID, f.findErr
}

func (f *fakeGateway) CreatePageClaudeLog(_ context.Context, dbID string, props notion.ClaudeLogProperties, _ []map[string]any) (string, error) {
	f.createCalls++
	f.lastDBID = dbID
	f.lastProps = props
	return f.createID, f.createErr
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRunner_CreatesPageWhenAbsent(t *testing.T) {
	g := &fakeGateway{createID: "page-created"}
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", newSilentLogger(), false)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Created || res.Skipped {
		t.Errorf("expected Created=true Skipped=false, got %+v", res)
	}
	if res.PageID != "page-created" {
		t.Errorf("PageID = %q", res.PageID)
	}
	if g.createCalls != 1 {
		t.Errorf("CreatePageClaudeLog calls = %d, want 1", g.createCalls)
	}
	if g.lastDBID != "claudelog-db" {
		t.Errorf("CreatePage dbID = %q, want claudelog-db", g.lastDBID)
	}
	if g.lastProps.SessionID != "sess-sample" {
		t.Errorf("created with SessionID=%q", g.lastProps.SessionID)
	}
}

func TestRunner_SkipsWhenPageExists(t *testing.T) {
	g := &fakeGateway{findID: "page-existing"}
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", newSilentLogger(), false)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Skipped || res.Created {
		t.Errorf("expected Skipped=true Created=false, got %+v", res)
	}
	if g.createCalls != 0 {
		t.Errorf("CreatePageClaudeLog should not be called, got %d", g.createCalls)
	}
	if res.PageID != "page-existing" {
		t.Errorf("PageID = %q", res.PageID)
	}
}

func TestRunner_DryRunSkipsGateway(t *testing.T) {
	g := &fakeGateway{}
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", newSilentLogger(), true)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Created || res.Skipped {
		t.Errorf("dry-run should leave Created/Skipped false, got %+v", res)
	}
	if g.verifyCalls != 0 || g.findCalls != 0 || g.createCalls != 0 {
		t.Errorf("gateway must not be called in dry-run: %+v", g)
	}
	if res.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", res.MessageCount)
	}
}

func TestRunner_SchemaIssuesError(t *testing.T) {
	g := &fakeGateway{
		verifyIssues: []notion.Issue{{Kind: notion.IssueMissing, Property: "Session ID", Want: "rich_text"}},
	}
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", newSilentLogger(), false)
	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error on schema issue")
	}
	if !strings.Contains(err.Error(), "Session ID") {
		t.Errorf("error should name the missing property, got: %v", err)
	}
	if g.createCalls != 0 {
		t.Errorf("must not create on schema failure: %d", g.createCalls)
	}
}

func TestRunner_VerifyErrorPropagates(t *testing.T) {
	g := &fakeGateway{verifyErr: errors.New("network down")}
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", newSilentLogger(), false)
	_, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "verify schema") {
		t.Fatalf("expected verify schema error, got %v", err)
	}
}
