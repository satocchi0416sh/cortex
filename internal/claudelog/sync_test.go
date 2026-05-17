package claudelog

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/satocchi0416sh/cortex/internal/notion"
)

type capturedCursor struct {
	UUID  string
	Count int
	Time  time.Time
}

type fakeGateway struct {
	verifyIssues []notion.Issue
	verifyErr    error
	findID       string
	findErr      error
	createID     string
	createErr    error

	getPageLastUUIDReturn string
	getPageLastUUIDErr    error
	appendErr             error
	updateCursorErr       error

	verifyCalls          int
	findCalls            int
	createCalls          int
	getPageLastUUIDCalls int
	appendCalls          int
	updateCursorCalls    int

	lastDBID         string
	lastProps        notion.ClaudeLogProperties
	lastAppendBlocks []map[string]any
	lastCursor       capturedCursor
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

func (f *fakeGateway) GetPageLastUUID(_ context.Context, _ string) (string, error) {
	f.getPageLastUUIDCalls++
	return f.getPageLastUUIDReturn, f.getPageLastUUIDErr
}

func (f *fakeGateway) AppendNewChildren(_ context.Context, _ string, blocks []map[string]any) error {
	f.appendCalls++
	f.lastAppendBlocks = blocks
	return f.appendErr
}

func (f *fakeGateway) UpdateClaudeLogCursorProperties(_ context.Context, _, lastUUID string, messageCount int, lastSynced time.Time) error {
	f.updateCursorCalls++
	f.lastCursor = capturedCursor{UUID: lastUUID, Count: messageCount, Time: lastSynced}
	return f.updateCursorErr
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newTempState(t *testing.T) *State {
	t.Helper()
	dir := t.TempDir()
	s, err := LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	return s
}

func TestRunner_CreatesPageWhenAbsent(t *testing.T) {
	g := &fakeGateway{createID: "page-created"}
	state := newTempState(t)
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", state, newSilentLogger(), false)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Created || res.Skipped || res.Updated {
		t.Errorf("expected Created=true Skipped=false Updated=false, got %+v", res)
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
	if g.updateCursorCalls != 1 {
		t.Errorf("UpdateClaudeLogCursorProperties calls = %d, want 1 (seed)", g.updateCursorCalls)
	}
}

// TestRunner_EmptySessionSkippedNoApiCall (Issue #22) guards the subagent
// scenario: a JSONL whose entries are all isSidechain=true (or empty)
// produces Session.Messages=0. Without the early-return, the runner would
// call CreatePage and seed an empty page that fails on every subsequent
// cycle with cursor-stale. The empty case must short-circuit before any
// gateway method is invoked.
func TestRunner_EmptySessionSkippedNoApiCall(t *testing.T) {
	g := &fakeGateway{createID: "should-not-be-called"}
	state := newTempState(t)
	r := NewRunner(g, "claudelog-db", "testdata/empty_sidechain_only.jsonl", state, newSilentLogger(), false)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run on empty session must not error: %v", err)
	}
	if res == nil {
		t.Fatal("expected RunResult, got nil")
	}
	if res.Created || res.Updated || res.Skipped {
		t.Errorf("expected all flags false on empty skip, got %+v", res)
	}
	if g.verifyCalls != 0 {
		t.Errorf("VerifyDatabaseSchemaWith calls = %d, want 0 (must not hit network)", g.verifyCalls)
	}
	if g.findCalls != 0 {
		t.Errorf("FindPageBySessionID calls = %d, want 0", g.findCalls)
	}
	if g.createCalls != 0 {
		t.Errorf("CreatePageClaudeLog calls = %d, want 0", g.createCalls)
	}
	if g.appendCalls != 0 {
		t.Errorf("AppendNewChildren calls = %d, want 0", g.appendCalls)
	}
	if g.updateCursorCalls != 0 {
		t.Errorf("UpdateClaudeLogCursorProperties calls = %d, want 0", g.updateCursorCalls)
	}
}

func TestRunner_SeedsCursorOnInitialCreate(t *testing.T) {
	g := &fakeGateway{createID: "page-created"}
	state := newTempState(t)
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", state, newSilentLogger(), false)
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if g.updateCursorCalls != 1 {
		t.Fatalf("updateCursorCalls = %d, want 1", g.updateCursorCalls)
	}
	if g.lastCursor.UUID != "uuid-asst-1" {
		t.Errorf("cursor UUID = %q, want uuid-asst-1 (last message)", g.lastCursor.UUID)
	}
	if g.lastCursor.Count != 2 {
		t.Errorf("cursor Count = %d, want 2", g.lastCursor.Count)
	}
	entry, ok := state.Get("sess-sample")
	if !ok {
		t.Fatal("state did not record entry")
	}
	if entry.LastUUID != "uuid-asst-1" || entry.MessageCount != 2 {
		t.Errorf("state entry = %+v, want LastUUID=uuid-asst-1 Count=2", entry)
	}
}

func TestRunner_AppendsNewMessagesWhenPageExists(t *testing.T) {
	g := &fakeGateway{findID: "page-1"}
	state := newTempState(t)
	state.Set("sess-ext", SessionEntry{
		PageID:       "page-1",
		LastUUID:     "uuid-2",
		MessageCount: 2,
	})
	r := NewRunner(g, "claudelog-db", "testdata/sample_extended.jsonl", state, newSilentLogger(), false)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Updated || res.Created || res.Skipped {
		t.Errorf("expected Updated=true, got %+v", res)
	}
	if g.appendCalls != 1 {
		t.Errorf("appendCalls = %d, want 1", g.appendCalls)
	}
	if g.updateCursorCalls != 1 {
		t.Errorf("updateCursorCalls = %d, want 1", g.updateCursorCalls)
	}
	if g.lastCursor.UUID != "uuid-5" {
		t.Errorf("cursor UUID = %q, want uuid-5", g.lastCursor.UUID)
	}
	if g.lastCursor.Count != 5 {
		t.Errorf("cursor Count = %d, want 5 (total)", g.lastCursor.Count)
	}
	if res.MessageCount != 3 {
		t.Errorf("RunResult.MessageCount = %d, want 3 (new only)", res.MessageCount)
	}
	if g.getPageLastUUIDCalls != 0 {
		t.Errorf("GetPageLastUUID called %d times, want 0 (state should win)", g.getPageLastUUIDCalls)
	}
	// Spot-check that uuid-3..5 content shows up in appended blocks. The
	// markdown converter may split "msg three" into separate text spans, so
	// we look for the distinctive token from each message individually.
	appended := stringifyBlocks(g.lastAppendBlocks)
	for _, want := range []string{"three", "four", "five"} {
		if !strings.Contains(appended, want) {
			t.Errorf("appended blocks missing %q; got %s", want, appended)
		}
	}
	for _, gone := range []string{"one", "two"} {
		if strings.Contains(appended, gone) {
			t.Errorf("appended blocks should not contain old %q (uuid-1/uuid-2 already synced)", gone)
		}
	}
}

func TestRunner_CursorFromPagePropertyWhenStateMissing(t *testing.T) {
	g := &fakeGateway{findID: "page-1", getPageLastUUIDReturn: "uuid-3"}
	state := newTempState(t)
	r := NewRunner(g, "claudelog-db", "testdata/sample_extended.jsonl", state, newSilentLogger(), false)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Updated {
		t.Errorf("expected Updated=true, got %+v", res)
	}
	if g.getPageLastUUIDCalls != 1 {
		t.Errorf("GetPageLastUUID calls = %d, want 1 (state empty so fall back to page)", g.getPageLastUUIDCalls)
	}
	if res.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2 (uuid-4..5)", res.MessageCount)
	}
	appended := stringifyBlocks(g.lastAppendBlocks)
	for _, want := range []string{"four", "five"} {
		if !strings.Contains(appended, want) {
			t.Errorf("appended blocks missing %q", want)
		}
	}
	if strings.Contains(appended, "three") {
		t.Errorf("appended blocks should start AFTER cursor uuid-3, found 'three'")
	}
}

func TestRunner_CursorStaleAborts(t *testing.T) {
	g := &fakeGateway{findID: "page-1"}
	state := newTempState(t)
	state.Set("sess-ext", SessionEntry{
		PageID:       "page-1",
		LastUUID:     "uuid-XYZ", // not present in sample_extended.jsonl
		MessageCount: 99,
	})
	r := NewRunner(g, "claudelog-db", "testdata/sample_extended.jsonl", state, newSilentLogger(), false)
	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on stale cursor")
	}
	if !errors.Is(err, ErrCursorStale) {
		t.Errorf("err = %v, want wraps ErrCursorStale", err)
	}
	if g.appendCalls != 0 {
		t.Errorf("appendCalls = %d, want 0", g.appendCalls)
	}
	if g.updateCursorCalls != 0 {
		t.Errorf("updateCursorCalls = %d, want 0 on stale", g.updateCursorCalls)
	}
}

// TestRunner_PageExistsCursorEmpty guards AC4's empty-cursor edge case: a page
// exists in Notion but neither the local state nor the Notion Last UUID
// property carries a cursor (e.g. an MVP-vintage page created before the
// append feature shipped). Silent full re-upload would violate AC2, so the
// runner must abort with ErrCursorStale and tell the user to delete the page.
func TestRunner_PageExistsCursorEmpty(t *testing.T) {
	g := &fakeGateway{findID: "page-1", getPageLastUUIDReturn: ""}
	state := newTempState(t) // empty state, sess-ext not set
	r := NewRunner(g, "claudelog-db", "testdata/sample_extended.jsonl", state, newSilentLogger(), false)
	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on missing cursor")
	}
	if !errors.Is(err, ErrCursorStale) {
		t.Errorf("err = %v, want wraps ErrCursorStale", err)
	}
	if !strings.Contains(err.Error(), "cursor missing") {
		t.Errorf("err = %v, want message containing 'cursor missing'", err)
	}
	if g.getPageLastUUIDCalls != 1 {
		t.Errorf("getPageLastUUIDCalls = %d, want 1 (state miss → property fallback)", g.getPageLastUUIDCalls)
	}
	if g.appendCalls != 0 {
		t.Errorf("appendCalls = %d, want 0 on empty cursor", g.appendCalls)
	}
	if g.updateCursorCalls != 0 {
		t.Errorf("updateCursorCalls = %d, want 0 on empty cursor", g.updateCursorCalls)
	}
}

func TestRunner_UpToDateUpdatesSyncedOnly(t *testing.T) {
	g := &fakeGateway{findID: "page-1"}
	state := newTempState(t)
	state.Set("sess-ext", SessionEntry{
		PageID:       "page-1",
		LastUUID:     "uuid-5",
		MessageCount: 5,
	})
	r := NewRunner(g, "claudelog-db", "testdata/sample_extended.jsonl", state, newSilentLogger(), false)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Skipped || res.Updated || res.Created {
		t.Errorf("expected Skipped=true, got %+v", res)
	}
	if g.appendCalls != 0 {
		t.Errorf("appendCalls = %d, want 0 (no new messages)", g.appendCalls)
	}
	if g.updateCursorCalls != 1 {
		t.Errorf("updateCursorCalls = %d, want 1 (Last Synced refresh)", g.updateCursorCalls)
	}
}

func TestRunner_DryRunSkipsGateway(t *testing.T) {
	g := &fakeGateway{}
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", nil, newSilentLogger(), true)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Created || res.Skipped || res.Updated {
		t.Errorf("dry-run should leave Created/Skipped/Updated false, got %+v", res)
	}
	if g.verifyCalls != 0 || g.findCalls != 0 || g.createCalls != 0 || g.appendCalls != 0 || g.updateCursorCalls != 0 {
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
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", nil, newSilentLogger(), false)
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
	r := NewRunner(g, "claudelog-db", "testdata/sample.jsonl", nil, newSilentLogger(), false)
	_, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "verify schema") {
		t.Fatalf("expected verify schema error, got %v", err)
	}
}

// stringifyBlocks walks the chunked-block representation and returns a single
// debug string containing all textual content. Lossy but enough to assert
// which messages survived the append slicing.
func stringifyBlocks(blocks []map[string]any) string {
	var sb strings.Builder
	for _, block := range blocks {
		writeBlockText(&sb, block)
	}
	return sb.String()
}

func writeBlockText(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case map[string]any:
		for _, val := range x {
			writeBlockText(sb, val)
		}
	case []any:
		for _, item := range x {
			writeBlockText(sb, item)
		}
	case []map[string]any:
		for _, item := range x {
			writeBlockText(sb, item)
		}
	case string:
		sb.WriteString(x)
		sb.WriteByte(' ')
	}
}
