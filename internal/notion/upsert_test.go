package notion

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClaudeLogProperties_toMap(t *testing.T) {
	started := time.Date(2026, 5, 17, 9, 0, 0, 0, time.UTC)
	synced := time.Date(2026, 5, 17, 9, 30, 0, 0, time.UTC)
	p := ClaudeLogProperties{
		Title:      "abc12345 dotgo",
		SessionID:  "abc12345-0000-0000-0000-000000000000",
		Project:    "/Users/me/Projects/dotgo",
		SourcePath: "/Users/me/.claude/projects/foo/abc12345.jsonl",
		StartedAt:  started,
		LastSynced: synced,
	}
	m := p.toMap()
	for _, name := range []string{"Name", "Session ID", "Project", "Source Path", "Started At", "Last Synced"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing key %q in toMap: %v", name, m)
		}
	}
	date, ok := m["Started At"].(map[string]any)["date"].(map[string]any)
	if !ok || date["start"] != started.Format(time.RFC3339) {
		t.Errorf("Started At not RFC3339 UTC: %v", m["Started At"])
	}
}

func TestClaudeLogProperties_toMap_omitsZeroDates(t *testing.T) {
	p := ClaudeLogProperties{
		Title:      "t",
		SessionID:  "s",
		Project:    "p",
		SourcePath: "sp",
	}
	m := p.toMap()
	if _, ok := m["Started At"]; ok {
		t.Errorf("Started At should be omitted when zero")
	}
	if _, ok := m["Last Synced"]; ok {
		t.Errorf("Last Synced should be omitted when zero")
	}
}

func TestFindPageBySessionID_filterBody(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/db123") {
			// Resolve to legacy /databases/<id> root so the query path stays
			// /databases/db123/query (matches the assertion below).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "db123",
				"properties": map[string]any{"Session ID": map[string]any{"id": "x", "name": "Session ID", "type": "rich_text"}},
			})
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/databases/db123/query") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"id": "page-1"}},
		})
	})
	pageID, err := c.FindPageBySessionID(context.Background(), "sess-xyz")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if pageID != "page-1" {
		t.Errorf("pageID = %q", pageID)
	}
	filter, ok := captured["filter"].(map[string]any)
	if !ok {
		t.Fatalf("missing filter: %v", captured)
	}
	if filter["property"] != "Session ID" {
		t.Errorf("property = %v", filter["property"])
	}
	rt, ok := filter["rich_text"].(map[string]any)
	if !ok {
		t.Fatalf("missing rich_text: %v", filter)
	}
	if rt["equals"] != "sess-xyz" {
		t.Errorf("equals = %v", rt["equals"])
	}
}

func TestFindPageBySessionID_emptyResult(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/db123") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "db123",
				"properties": map[string]any{"Session ID": map[string]any{"id": "x", "name": "Session ID", "type": "rich_text"}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	})
	pageID, err := c.FindPageBySessionID(context.Background(), "nope")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if pageID != "" {
		t.Errorf("expected empty page id, got %q", pageID)
	}
}

func TestCreatePageClaudeLog_usesArgDBID(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/claudelog-db") {
			// Legacy-style DB (properties on the DB itself) → dbParent returns
			// {"database_id": ...} and the assertion below stays unchanged.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "claudelog-db",
				"properties": map[string]any{"Name": map[string]any{"id": "t", "name": "Name", "type": "title"}},
			})
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/pages") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-new"})
	})
	props := ClaudeLogProperties{
		Title:      "t",
		SessionID:  "s",
		Project:    "p",
		SourcePath: "sp",
	}
	pageID, err := c.CreatePageClaudeLog(context.Background(), "claudelog-db", props, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if pageID != "page-new" {
		t.Errorf("pageID = %q", pageID)
	}
	parent, ok := captured["parent"].(map[string]any)
	if !ok {
		t.Fatalf("missing parent: %v", captured)
	}
	if parent["database_id"] != "claudelog-db" {
		t.Errorf("parent.database_id = %v, want claudelog-db (must use arg, not client default db123)", parent["database_id"])
	}
}

// TestCreatePageClaudeLog_DataSourceParent (Issue #8): when the target DB is a
// post-2025-09-03 multi-data_source database, the page POST must use
// parent.data_source_id (legacy database_id returns 400 invalid_request_url
// from Notion).
func TestCreatePageClaudeLog_DataSourceParent(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/new-style-db") {
			// New-style DB: properties live on a data_source, not on the DB.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "new-style-db",
				"properties":   map[string]any{},
				"data_sources": []map[string]any{{"id": "ds-7", "name": "primary"}},
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/data_sources/ds-7") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"properties": map[string]any{"Name": map[string]any{"id": "t", "name": "Name", "type": "title"}},
			})
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/pages") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-ds"})
	})
	if _, err := c.CreatePageClaudeLog(context.Background(), "new-style-db", ClaudeLogProperties{Title: "t"}, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	parent, ok := captured["parent"].(map[string]any)
	if !ok {
		t.Fatalf("missing parent: %v", captured)
	}
	if got := parent["data_source_id"]; got != "ds-7" {
		t.Errorf("parent.data_source_id = %v, want ds-7 (multi data_source DB must not use database_id)", got)
	}
	if _, present := parent["database_id"]; present {
		t.Errorf("parent.database_id must not be present alongside data_source_id: %v", parent)
	}
}

// TestCreatePage_DataSourceParent (Issue #8, markdown path): same fix must
// apply to the existing CreatePage entry used by markdown sync; otherwise a
// user pointing the markdown DB at a new-style Notion DB would silently break.
func TestCreatePage_DataSourceParent(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/db123") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "db123",
				"properties":   map[string]any{},
				"data_sources": []map[string]any{{"id": "ds-md", "name": "primary"}},
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/data_sources/ds-md") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"properties": map[string]any{"Name": map[string]any{"id": "t", "name": "Name", "type": "title"}},
			})
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/pages") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-md"})
	})
	if _, err := c.CreatePage(context.Background(), Properties{Title: "t"}, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	parent, ok := captured["parent"].(map[string]any)
	if !ok {
		t.Fatalf("missing parent: %v", captured)
	}
	if parent["data_source_id"] != "ds-md" {
		t.Errorf("parent.data_source_id = %v, want ds-md", parent["data_source_id"])
	}
}

// TestCreatePage_LegacyDatabaseIDParent (Issue #8 regression): legacy DBs (no
// data_sources array) keep using parent.database_id — the markdown sync path
// against pre-2025-09-03 DBs must remain byte-identical.
func TestCreatePage_LegacyDatabaseIDParent(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/db123") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "db123",
				"properties": map[string]any{"Name": map[string]any{"id": "t", "name": "Name", "type": "title"}},
			})
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/pages") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-legacy"})
	})
	if _, err := c.CreatePage(context.Background(), Properties{Title: "t"}, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	parent, ok := captured["parent"].(map[string]any)
	if !ok {
		t.Fatalf("missing parent: %v", captured)
	}
	if parent["database_id"] != "db123" {
		t.Errorf("parent.database_id = %v, want db123 (legacy DBs must stay byte-identical)", parent["database_id"])
	}
	if _, present := parent["data_source_id"]; present {
		t.Errorf("parent.data_source_id must not appear for legacy DB: %v", parent)
	}
}

func TestAppendNewChildren_ChunksAt100(t *testing.T) {
	var patchCount int
	var chunkSizes []int
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/children") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Children []map[string]any `json:"children"`
		}
		_ = json.Unmarshal(body, &payload)
		patchCount++
		chunkSizes = append(chunkSizes, len(payload.Children))
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list"})
	})
	blocks := make([]map[string]any, 201)
	for i := range blocks {
		blocks[i] = map[string]any{"object": "block", "type": "paragraph"}
	}
	if err := c.AppendNewChildren(context.Background(), "page-1", blocks); err != nil {
		t.Fatalf("AppendNewChildren: %v", err)
	}
	if patchCount != 3 {
		t.Errorf("PATCH count = %d, want 3 (201 blocks / 100)", patchCount)
	}
	for i, n := range chunkSizes {
		if n > 100 {
			t.Errorf("chunk[%d] size = %d, want <=100", i, n)
		}
	}
	if chunkSizes[0]+chunkSizes[1]+chunkSizes[2] != 201 {
		t.Errorf("total blocks across chunks = %d, want 201", chunkSizes[0]+chunkSizes[1]+chunkSizes[2])
	}
}

func TestGetPageLastUUID_ExtractsRichText(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/pages/page-1") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "page-1",
			"properties": map[string]any{
				"Last UUID": map[string]any{
					"rich_text": []map[string]any{
						{
							"type":       "text",
							"plain_text": "uuid-7",
							"text":       map[string]any{"content": "uuid-7"},
						},
					},
				},
			},
		})
	})
	got, err := c.GetPageLastUUID(context.Background(), "page-1")
	if err != nil {
		t.Fatalf("GetPageLastUUID: %v", err)
	}
	if got != "uuid-7" {
		t.Errorf("got %q, want uuid-7", got)
	}
}

func TestGetPageLastUUID_EmptyWhenAbsent(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "page-1",
			"properties": map[string]any{},
		})
	})
	got, err := c.GetPageLastUUID(context.Background(), "page-1")
	if err != nil {
		t.Fatalf("GetPageLastUUID: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestUpdateClaudeLogCursorProperties_BodyShape(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/pages/page-1") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-1"})
	})
	when := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 17, 9, 30, 0, 0, time.UTC)
	if err := c.UpdateClaudeLogCursorProperties(context.Background(), "page-1", "uuid-9", 12, when, updated); err != nil {
		t.Fatalf("UpdateClaudeLogCursorProperties: %v", err)
	}
	props, ok := captured["properties"].(map[string]any)
	if !ok {
		t.Fatalf("missing properties: %v", captured)
	}
	allowed := map[string]struct{}{"Last UUID": {}, "Message Count": {}, "Last Synced": {}, "Last Updated": {}}
	for name := range props {
		if _, ok := allowed[name]; !ok {
			t.Errorf("body contains forbidden property %q (partial PATCH must be cursor-only)", name)
		}
	}
	for name := range allowed {
		if _, ok := props[name]; !ok {
			t.Errorf("body missing required property %q", name)
		}
	}
	cnt, ok := props["Message Count"].(map[string]any)["number"].(float64)
	if !ok || int(cnt) != 12 {
		t.Errorf("Message Count = %v, want 12", props["Message Count"])
	}
	date, ok := props["Last Synced"].(map[string]any)["date"].(map[string]any)
	if !ok || date["start"] != when.Format(time.RFC3339) {
		t.Errorf("Last Synced = %v", props["Last Synced"])
	}
	upDate, ok := props["Last Updated"].(map[string]any)["date"].(map[string]any)
	if !ok || upDate["start"] != updated.Format(time.RFC3339) {
		t.Errorf("Last Updated = %v, want %v", props["Last Updated"], updated.Format(time.RFC3339))
	}
}

// TestUpdateClaudeLogCursorProperties_OmitsLastUpdatedOnZero (Issue #24):
// passing zero-value lastUpdated must omit "Last Updated" from PATCH body so
// existing values are preserved during cursor-only no-op refresh cycles.
func TestUpdateClaudeLogCursorProperties_OmitsLastUpdatedOnZero(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-1"})
	})
	when := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	if err := c.UpdateClaudeLogCursorProperties(context.Background(), "page-1", "uuid-9", 12, when, time.Time{}); err != nil {
		t.Fatalf("UpdateClaudeLogCursorProperties: %v", err)
	}
	props, _ := captured["properties"].(map[string]any)
	if _, present := props["Last Updated"]; present {
		t.Errorf("Last Updated must be omitted when zero, got %v", props["Last Updated"])
	}
	if _, ok := props["Last Synced"]; !ok {
		t.Errorf("Last Synced still required when only lastUpdated is zero")
	}
}

func TestFindPageByExternalID_stillWorks(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/db123") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "db123",
				"properties": map[string]any{"External ID": map[string]any{"id": "x", "name": "External ID", "type": "rich_text"}},
			})
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"id": "page-ext"}},
		})
	})
	pageID, err := c.FindPageByExternalID(context.Background(), "ext-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if pageID != "page-ext" {
		t.Errorf("pageID = %q", pageID)
	}
	filter, _ := captured["filter"].(map[string]any)
	if filter["property"] != "External ID" {
		t.Errorf("property = %v, regression in FindPageByExternalID", filter["property"])
	}
}
