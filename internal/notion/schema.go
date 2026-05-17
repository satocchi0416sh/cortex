package notion

import (
	"context"
	"fmt"
	"sort"
)

// PropDef describes a required Notion DB property.
type PropDef struct {
	Name string
	Type string // notion property type: title, rich_text, date, number, ...
}

// IssueKind enumerates schema problems doctor reports.
type IssueKind string

const (
	IssueMissing      IssueKind = "missing"
	IssueTypeMismatch IssueKind = "type_mismatch"
)

// Issue is a single schema problem.
type Issue struct {
	Kind     IssueKind
	Property string
	Want     string
	Got      string // empty for IssueMissing
}

func (i Issue) String() string {
	switch i.Kind {
	case IssueMissing:
		return fmt.Sprintf("missing property %q (want %s)", i.Property, i.Want)
	case IssueTypeMismatch:
		return fmt.Sprintf("property %q has type %q, want %q", i.Property, i.Got, i.Want)
	default:
		return fmt.Sprintf("%s on %s", i.Kind, i.Property)
	}
}

// RequiredProperties is the canonical schema cortex-sync expects.
var RequiredProperties = []PropDef{
	{Name: "Name", Type: "title"},
	{Name: "External ID", Type: "rich_text"},
	{Name: "Project", Type: "rich_text"},
	{Name: "File Path", Type: "rich_text"},
	{Name: "Content Hash", Type: "rich_text"},
	{Name: "Last Synced", Type: "date"},
}

// ClaudeLogRequiredProperties is the canonical schema the claude-log
// subcommand expects on the dedicated Notion DB.
var ClaudeLogRequiredProperties = []PropDef{
	{Name: "Name", Type: "title"},
	{Name: "Session ID", Type: "rich_text"},
	{Name: "Project", Type: "rich_text"},
	{Name: "Started At", Type: "date"},
	{Name: "Source Path", Type: "rich_text"},
	{Name: "Last Synced", Type: "date"},
}

// DatabaseInfo is the subset of GET /v1/databases/{id} we care about.
// Post 2025-09-03 the properties live on a data_source rather than the
// database itself; schemaPath captures the endpoint to PATCH for schema edits.
type DatabaseInfo struct {
	ID         string                  `json:"id"`
	Properties map[string]propertyMeta `json:"properties"`

	schemaPath string
}

type propertyMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type dataSourceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type rawDatabase struct {
	ID          string                  `json:"id"`
	Properties  map[string]propertyMeta `json:"properties"`
	DataSources []dataSourceRef         `json:"data_sources"`
}

// GetDatabase fetches the DB metadata, following data_sources when the
// database itself carries no properties (Notion 2025-09-03 upgrade).
func (c *Client) GetDatabase(ctx context.Context, dbID string) (*DatabaseInfo, error) {
	if dbID == "" {
		dbID = c.dbID
	}
	var raw rawDatabase
	if err := c.do(ctx, "GET", "/databases/"+dbID, nil, &raw); err != nil {
		return nil, err
	}
	info := &DatabaseInfo{
		ID:         raw.ID,
		Properties: raw.Properties,
		schemaPath: "/databases/" + dbID,
	}
	if len(info.Properties) == 0 && len(raw.DataSources) > 0 {
		dsID := raw.DataSources[0].ID
		var ds rawDatabase
		if err := c.do(ctx, "GET", "/data_sources/"+dsID, nil, &ds); err != nil {
			return nil, fmt.Errorf("get data_source %s: %w", dsID, err)
		}
		info.Properties = ds.Properties
		info.schemaPath = "/data_sources/" + dsID
	}
	return info, nil
}

// VerifyDatabaseSchema returns issues found relative to RequiredProperties.
// A nil/empty issue slice means the schema is compatible.
func (c *Client) VerifyDatabaseSchema(ctx context.Context, dbID string) ([]Issue, error) {
	return c.VerifyDatabaseSchemaWith(ctx, dbID, RequiredProperties)
}

// VerifyDatabaseSchemaWith returns issues found relative to the supplied
// required-property list. Use this when a subcommand (e.g. claude-log) needs a
// different schema than the default markdown sync.
func (c *Client) VerifyDatabaseSchemaWith(ctx context.Context, dbID string, required []PropDef) ([]Issue, error) {
	info, err := c.GetDatabase(ctx, dbID)
	if err != nil {
		return nil, err
	}
	return diffSchema(info, required), nil
}

func diffSchema(info *DatabaseInfo, required []PropDef) []Issue {
	var issues []Issue
	for _, want := range required {
		got, ok := info.Properties[want.Name]
		if !ok {
			issues = append(issues, Issue{Kind: IssueMissing, Property: want.Name, Want: want.Type})
			continue
		}
		if got.Type != want.Type {
			issues = append(issues, Issue{
				Kind:     IssueTypeMismatch,
				Property: want.Name,
				Want:     want.Type,
				Got:      got.Type,
			})
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Kind != issues[j].Kind {
			return issues[i].Kind < issues[j].Kind
		}
		return issues[i].Property < issues[j].Property
	})
	return issues
}

// EnsureProperties PATCHes the DB (or the underlying data_source on the
// 2025-09-03 API) to add any missing properties. Type mismatches are NOT
// auto-corrected.
func (c *Client) EnsureProperties(ctx context.Context, dbID string, missing []PropDef) error {
	if len(missing) == 0 {
		return nil
	}
	info, err := c.GetDatabase(ctx, dbID)
	if err != nil {
		return err
	}
	props := map[string]any{}
	for _, p := range missing {
		props[p.Name] = map[string]any{p.Type: map[string]any{}}
	}
	return c.do(ctx, "PATCH", info.schemaPath, map[string]any{"properties": props}, nil)
}

// MissingFromIssues filters issues down to the missing ones, returning a
// PropDef list suitable for EnsureProperties.
func MissingFromIssues(issues []Issue) []PropDef {
	out := make([]PropDef, 0, len(issues))
	for _, iss := range issues {
		if iss.Kind == IssueMissing {
			out = append(out, PropDef{Name: iss.Property, Type: iss.Want})
		}
	}
	return out
}
