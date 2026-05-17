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

func TestFindPageByExternalID_stillWorks(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
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
