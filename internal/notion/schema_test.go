package notion

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newFakeServer(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(Options{
		Token:      "test-token",
		DatabaseID: "db123",
		RPS:        100,
		HTTPClient: srv.Client(),
	})
	c.http = &http.Client{Transport: rewriteHost{base: srv.Client().Transport, target: srv.URL}}
	// Re-attach header injection so token + version land in requests.
	c.http.Transport = &headerTransport{base: c.http.Transport, token: "test-token", version: notionVersion}
	return c, srv
}

// rewriteHost makes requests aimed at https://api.notion.com/v1/... resolve to
// the test server.
type rewriteHost struct {
	base   http.RoundTripper
	target string
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace scheme + host with the httptest server, preserving path.
	req2 := req.Clone(req.Context())
	target := strings.TrimRight(r.target, "/")
	// httptest URL is like http://127.0.0.1:54321
	parts := strings.SplitN(target, "://", 2)
	if len(parts) == 2 {
		req2.URL.Scheme = parts[0]
		req2.URL.Host = parts[1]
	}
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

func TestVerifyDatabaseSchema_AllPresent(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/databases/db123") {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "db123",
			"properties": map[string]any{
				"Name":         map[string]any{"id": "1", "name": "Name", "type": "title"},
				"External ID":  map[string]any{"id": "2", "name": "External ID", "type": "rich_text"},
				"Project":      map[string]any{"id": "3", "name": "Project", "type": "rich_text"},
				"File Path":    map[string]any{"id": "4", "name": "File Path", "type": "rich_text"},
				"Content Hash": map[string]any{"id": "5", "name": "Content Hash", "type": "rich_text"},
				"Last Synced":  map[string]any{"id": "6", "name": "Last Synced", "type": "date"},
			},
		})
	})

	issues, err := c.VerifyDatabaseSchema(context.Background(), "db123")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestVerifyDatabaseSchema_Missing(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "db123",
			"properties": map[string]any{
				"Name":        map[string]any{"id": "1", "name": "Name", "type": "title"},
				"External ID": map[string]any{"id": "2", "name": "External ID", "type": "rich_text"},
				"File Path":   map[string]any{"id": "4", "name": "File Path", "type": "rich_text"},
				"Last Synced": map[string]any{"id": "6", "name": "Last Synced", "type": "date"},
				// Project, Content Hash missing
			},
		})
	})

	issues, err := c.VerifyDatabaseSchema(context.Background(), "db123")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %v", len(issues), issues)
	}
	missing := MissingFromIssues(issues)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing, got %d", len(missing))
	}
	wantNames := map[string]string{"Project": "rich_text", "Content Hash": "rich_text"}
	for _, m := range missing {
		if wantNames[m.Name] != m.Type {
			t.Errorf("unexpected missing property: %+v", m)
		}
	}
}

func TestVerifyDatabaseSchema_TypeMismatch(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "db123",
			"properties": map[string]any{
				"Name":         map[string]any{"id": "1", "name": "Name", "type": "title"},
				"External ID":  map[string]any{"id": "2", "name": "External ID", "type": "rich_text"},
				"Project":      map[string]any{"id": "3", "name": "Project", "type": "rich_text"},
				"File Path":    map[string]any{"id": "4", "name": "File Path", "type": "rich_text"},
				"Content Hash": map[string]any{"id": "5", "name": "Content Hash", "type": "number"}, // wrong
				"Last Synced":  map[string]any{"id": "6", "name": "Last Synced", "type": "date"},
			},
		})
	})

	issues, err := c.VerifyDatabaseSchema(context.Background(), "db123")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != IssueTypeMismatch {
		t.Errorf("kind=%v want type_mismatch", issues[0].Kind)
	}
	if issues[0].Property != "Content Hash" {
		t.Errorf("property=%q", issues[0].Property)
	}
	if issues[0].Got != "number" || issues[0].Want != "rich_text" {
		t.Errorf("got=%q want=%q", issues[0].Got, issues[0].Want)
	}
	if len(MissingFromIssues(issues)) != 0 {
		t.Errorf("type mismatch should not show as missing")
	}
}

func TestEnsureProperties_PatchBody(t *testing.T) {
	var captured map[string]any
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "db123",
				"properties": map[string]any{
					"Name": map[string]any{"id": "1", "name": "Name", "type": "title"},
				},
			})
			return
		case "PATCH":
			if !strings.HasSuffix(r.URL.Path, "/databases/db123") {
				t.Errorf("unexpected PATCH path: %s", r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &captured)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "db123"})
			return
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})

	err := c.EnsureProperties(context.Background(), "db123", []PropDef{
		{Name: "Project", Type: "rich_text"},
		{Name: "Content Hash", Type: "rich_text"},
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	props, ok := captured["properties"].(map[string]any)
	if !ok {
		t.Fatalf("missing properties in body: %v", captured)
	}
	for _, name := range []string{"Project", "Content Hash"} {
		entry, ok := props[name].(map[string]any)
		if !ok {
			t.Errorf("missing %q in patch body", name)
			continue
		}
		if _, ok := entry["rich_text"]; !ok {
			t.Errorf("%q expected rich_text key, got %v", name, entry)
		}
	}
}

func TestVerifyDatabaseSchema_FollowsDataSource(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/databases/db123"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "db123",
				"properties":   map[string]any{},
				"data_sources": []map[string]any{{"id": "ds-abc", "name": "DB"}},
			})
		case strings.HasSuffix(r.URL.Path, "/data_sources/ds-abc"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "ds-abc",
				"properties": map[string]any{
					"Name":         map[string]any{"id": "1", "name": "Name", "type": "title"},
					"External ID":  map[string]any{"id": "2", "name": "External ID", "type": "rich_text"},
					"Project":      map[string]any{"id": "3", "name": "Project", "type": "rich_text"},
					"File Path":    map[string]any{"id": "4", "name": "File Path", "type": "rich_text"},
					"Content Hash": map[string]any{"id": "5", "name": "Content Hash", "type": "rich_text"},
					"Last Synced":  map[string]any{"id": "6", "name": "Last Synced", "type": "date"},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	issues, err := c.VerifyDatabaseSchema(context.Background(), "db123")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues via data_source fallback, got %v", issues)
	}
}

func TestEnsureProperties_PatchesDataSource(t *testing.T) {
	var patchedPath string
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/databases/db123"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "db123",
				"properties":   map[string]any{},
				"data_sources": []map[string]any{{"id": "ds-abc"}},
			})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/data_sources/ds-abc"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ds-abc", "properties": map[string]any{}})
		case r.Method == "PATCH":
			patchedPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ds-abc"})
		default:
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.EnsureProperties(context.Background(), "db123", []PropDef{{Name: "Project", Type: "rich_text"}}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !strings.HasSuffix(patchedPath, "/data_sources/ds-abc") {
		t.Errorf("expected PATCH on data_sources, got %s", patchedPath)
	}
}

func TestEnsureProperties_NoOpForEmpty(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("should not call API when missing list is empty: %s %s", r.Method, r.URL.Path)
	})
	if err := c.EnsureProperties(context.Background(), "db123", nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}
}

func claudeLogDBPayload(overrides map[string]map[string]any) map[string]any {
	props := map[string]map[string]any{
		"Name":        {"id": "1", "name": "Name", "type": "title"},
		"Session ID":  {"id": "2", "name": "Session ID", "type": "rich_text"},
		"Project":     {"id": "3", "name": "Project", "type": "rich_text"},
		"Started At":  {"id": "4", "name": "Started At", "type": "date"},
		"Source Path": {"id": "5", "name": "Source Path", "type": "rich_text"},
		"Last Synced": {"id": "6", "name": "Last Synced", "type": "date"},
	}
	for name, replacement := range overrides {
		if replacement == nil {
			delete(props, name)
			continue
		}
		props[name] = replacement
	}
	out := map[string]any{"id": "db123", "properties": map[string]any{}}
	for name, p := range props {
		out["properties"].(map[string]any)[name] = p
	}
	return out
}

func TestDiffSchema_ClaudeLogAllPresent(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(claudeLogDBPayload(nil))
	})
	issues, err := c.VerifyDatabaseSchemaWith(context.Background(), "db123", ClaudeLogRequiredProperties)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestDiffSchema_ClaudeLogMissing(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(claudeLogDBPayload(map[string]map[string]any{
			"Session ID":  nil,
			"Source Path": nil,
		}))
	})
	issues, err := c.VerifyDatabaseSchemaWith(context.Background(), "db123", ClaudeLogRequiredProperties)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %v", len(issues), issues)
	}
	missing := MissingFromIssues(issues)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing, got %d", len(missing))
	}
	wantNames := map[string]string{"Session ID": "rich_text", "Source Path": "rich_text"}
	for _, m := range missing {
		if wantNames[m.Name] != m.Type {
			t.Errorf("unexpected missing property: %+v", m)
		}
	}
}

func TestDiffSchema_ClaudeLogTypeMismatch(t *testing.T) {
	c, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(claudeLogDBPayload(map[string]map[string]any{
			"Session ID": {"id": "2", "name": "Session ID", "type": "date"},
		}))
	})
	issues, err := c.VerifyDatabaseSchemaWith(context.Background(), "db123", ClaudeLogRequiredProperties)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(issues) != 1 || issues[0].Kind != IssueTypeMismatch {
		t.Fatalf("expected 1 type_mismatch, got %v", issues)
	}
	if issues[0].Property != "Session ID" || issues[0].Got != "date" || issues[0].Want != "rich_text" {
		t.Errorf("unexpected mismatch: %+v", issues[0])
	}
}
