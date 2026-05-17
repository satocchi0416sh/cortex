package notion

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Page struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
}

type queryResult struct {
	Results []Page `json:"results"`
}

type childrenResult struct {
	Results    []Page `json:"results"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

type Properties struct {
	Title       string
	ExternalID  string
	Project     string
	FilePath    string
	ContentHash string
	LastSynced  time.Time
}

func (p Properties) toMap() map[string]any {
	return map[string]any{
		"Name": map[string]any{
			"title": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.Title}},
			},
		},
		"External ID": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.ExternalID}},
			},
		},
		"Project": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.Project}},
			},
		},
		"File Path": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.FilePath}},
			},
		},
		"Content Hash": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.ContentHash}},
			},
		},
		"Last Synced": map[string]any{
			"date": map[string]any{"start": p.LastSynced.UTC().Format(time.RFC3339)},
		},
	}
}

func (c *Client) FindPageByExternalID(ctx context.Context, externalID string) (string, error) {
	return c.findPageByRichText(ctx, "External ID", externalID)
}

// FindPageBySessionID looks up an existing claude-log page by its "Session ID"
// rich_text property in the client's configured database. Returns the empty
// string when nothing matches.
func (c *Client) FindPageBySessionID(ctx context.Context, sessionID string) (string, error) {
	return c.findPageByRichText(ctx, "Session ID", sessionID)
}

// findPageByRichText runs a Notion DB query filtered on a rich_text property's
// "equals" predicate and returns the first matching page ID, or the empty
// string when nothing matches. Shared by FindPageByExternalID and
// FindPageBySessionID so the request shape stays canonical. The query path is
// resolved per database so pre-2025-09-03 databases keep using
// /databases/{id}/query while data_source-style databases use
// /data_sources/{ds_id}/query (otherwise Notion returns 400 invalid_request_url).
func (c *Client) findPageByRichText(ctx context.Context, propertyName, value string) (string, error) {
	root, err := c.dbRoot(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"filter": map[string]any{
			"property": propertyName,
			"rich_text": map[string]any{
				"equals": value,
			},
		},
		"page_size": 1,
	}
	var resp queryResult
	if err := c.do(ctx, "POST", root+"/query", body, &resp); err != nil {
		return "", err
	}
	if len(resp.Results) == 0 {
		return "", nil
	}
	return resp.Results[0].ID, nil
}

// dbRoot returns the Notion API path that is the source of truth for the
// client's database: "/databases/<id>" for pre-2025-09-03 databases where
// properties live on the DB itself, "/data_sources/<ds_id>" for newer
// databases where properties live on a data source. The result is cached
// per-client so subsequent queries skip the discovery GET.
func (c *Client) dbRoot(ctx context.Context) (string, error) {
	if v := c.cachedRoot.Load(); v != nil {
		return v.(string), nil
	}
	info, err := c.GetDatabase(ctx, c.dbID)
	if err != nil {
		return "", fmt.Errorf("resolve db root: %w", err)
	}
	c.cachedRoot.Store(info.schemaPath)
	return info.schemaPath, nil
}

// dbParent returns the Notion "parent" object that page-create endpoints
// expect for the supplied database. For pre-2025-09-03 DBs and single
// data_source DBs Notion still accepts {"database_id": dbID} (auto-resolved),
// but multi data_source DBs require an explicit {"data_source_id": <ds_id>}
// (otherwise the page POST returns 400). The helper transparently handles
// both shapes by inspecting the dbRoot / GetDatabase result.
//
// Caching: when dbID equals the client's configured database, the cached
// root populated by dbRoot is reused (no extra GET). For ad-hoc dbIDs
// (e.g. CreatePageClaudeLog routing to a non-default DB) a fresh
// GetDatabase is issued — page-create flows are already in the seconds-
// per-call range, so the extra round trip is acceptable.
func (c *Client) dbParent(ctx context.Context, dbID string) (map[string]any, error) {
	var root string
	if dbID == c.dbID {
		r, err := c.dbRoot(ctx)
		if err != nil {
			return nil, err
		}
		root = r
	} else {
		info, err := c.GetDatabase(ctx, dbID)
		if err != nil {
			return nil, fmt.Errorf("resolve db parent: %w", err)
		}
		root = info.schemaPath
	}
	if strings.HasPrefix(root, "/data_sources/") {
		return map[string]any{"data_source_id": strings.TrimPrefix(root, "/data_sources/")}, nil
	}
	return map[string]any{"database_id": dbID}, nil
}

func (c *Client) CreatePage(ctx context.Context, props Properties, blocks []map[string]any) (string, error) {
	parent, err := c.dbParent(ctx, c.dbID)
	if err != nil {
		return "", err
	}
	chunks := ChunkBlocks(blocks)
	var first []map[string]any
	if len(chunks) > 0 {
		first = chunks[0]
	}
	body := map[string]any{
		"parent":     parent,
		"properties": props.toMap(),
		"children":   first,
	}
	var resp Page
	if err := c.do(ctx, "POST", "/pages", body, &resp); err != nil {
		return "", err
	}
	if len(chunks) > 1 {
		for _, chunk := range chunks[1:] {
			if err := c.AppendChildren(ctx, resp.ID, chunk); err != nil {
				return resp.ID, err
			}
		}
	}
	return resp.ID, nil
}

func (c *Client) UpdatePageProperties(ctx context.Context, pageID string, props Properties) error {
	body := map[string]any{
		"properties": props.toMap(),
	}
	return c.do(ctx, "PATCH", "/pages/"+pageID, body, nil)
}

func (c *Client) AppendChildren(ctx context.Context, blockID string, children []map[string]any) error {
	body := map[string]any{"children": children}
	return c.do(ctx, "PATCH", "/blocks/"+blockID+"/children", body, nil)
}

func (c *Client) ListChildIDs(ctx context.Context, blockID string) ([]string, error) {
	var ids []string
	cursor := ""
	for {
		path := "/blocks/" + blockID + "/children?page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + cursor
		}
		var resp childrenResult
		if err := c.do(ctx, "GET", path, nil, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.Results {
			ids = append(ids, r.ID)
		}
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}
	return ids, nil
}

func (c *Client) DeleteBlock(ctx context.Context, blockID string) error {
	return c.do(ctx, "DELETE", "/blocks/"+blockID, nil, nil)
}

func (c *Client) ReplaceChildren(ctx context.Context, pageID string, blocks []map[string]any) error {
	ids, err := c.ListChildIDs(ctx, pageID)
	if err != nil {
		return fmt.Errorf("list children: %w", err)
	}
	for _, id := range ids {
		if err := c.DeleteBlock(ctx, id); err != nil {
			return fmt.Errorf("delete block %s: %w", id, err)
		}
	}
	for _, chunk := range ChunkBlocks(blocks) {
		if err := c.AppendChildren(ctx, pageID, chunk); err != nil {
			return fmt.Errorf("append children: %w", err)
		}
	}
	return nil
}

// ClaudeLogProperties captures the schema the claude-log subcommand writes to
// its dedicated Notion DB. Fields with zero values are omitted from the
// emitted map so partial updates do not clobber existing data. In particular
// LastUUID == "" and MessageCount == 0 are treated as "don't write this
// cursor field" so initial CreatePage calls (which set Title/SessionID/etc.
// but not yet the cursor) do not zero out an existing cursor on re-create.
type ClaudeLogProperties struct {
	Title        string
	SessionID    string
	Project      string
	SourcePath   string
	StartedAt    time.Time
	LastSynced   time.Time
	LastUUID     string
	MessageCount int
}

func (p ClaudeLogProperties) toMap() map[string]any {
	out := map[string]any{
		"Name": map[string]any{
			"title": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.Title}},
			},
		},
		"Session ID": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.SessionID}},
			},
		},
		"Project": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.Project}},
			},
		},
		"Source Path": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.SourcePath}},
			},
		},
	}
	if !p.StartedAt.IsZero() {
		out["Started At"] = map[string]any{
			"date": map[string]any{"start": p.StartedAt.UTC().Format(time.RFC3339)},
		}
	}
	if !p.LastSynced.IsZero() {
		out["Last Synced"] = map[string]any{
			"date": map[string]any{"start": p.LastSynced.UTC().Format(time.RFC3339)},
		}
	}
	if p.LastUUID != "" {
		out["Last UUID"] = map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": p.LastUUID}},
			},
		}
	}
	// MessageCount == 0 means "session has no messages yet" which is
	// effectively the same as "don't bother writing a cursor"; treat it as
	// omit so we never clobber a non-zero count with a stale zero.
	if p.MessageCount != 0 {
		out["Message Count"] = map[string]any{"number": p.MessageCount}
	}
	return out
}

// AppendNewChildren PATCH-es block children onto an existing page in chunks of
// blocksPerRequest. Unlike ReplaceChildren this is purely additive — existing
// children (including user-authored comments) are never touched. Used by the
// claude-log incremental sync path. Returns nil for an empty input slice.
func (c *Client) AppendNewChildren(ctx context.Context, pageID string, blocks []map[string]any) error {
	for _, chunk := range ChunkBlocks(blocks) {
		if err := c.AppendChildren(ctx, pageID, chunk); err != nil {
			return err
		}
	}
	return nil
}

// GetPageLastUUID fetches the page and extracts the "Last UUID" rich_text
// property's plain text. Returns "" (not an error) when the property is
// absent, empty, or unparseable so callers can use it as an AC5 fallback when
// no local state file is present.
func (c *Client) GetPageLastUUID(ctx context.Context, pageID string) (string, error) {
	var resp struct {
		Properties map[string]any `json:"properties"`
	}
	if err := c.do(ctx, "GET", "/pages/"+pageID, nil, &resp); err != nil {
		return "", err
	}
	prop, ok := resp.Properties["Last UUID"].(map[string]any)
	if !ok {
		return "", nil
	}
	rtRaw, ok := prop["rich_text"].([]any)
	if !ok || len(rtRaw) == 0 {
		return "", nil
	}
	first, ok := rtRaw[0].(map[string]any)
	if !ok {
		return "", nil
	}
	// Prefer plain_text (Notion's flattened convenience field); fall back to
	// text.content for clients/tests that don't synthesise it.
	if plain, ok := first["plain_text"].(string); ok && plain != "" {
		return plain, nil
	}
	textObj, ok := first["text"].(map[string]any)
	if !ok {
		return "", nil
	}
	content, _ := textObj["content"].(string)
	return content, nil
}

// UpdateClaudeLogCursorProperties PATCH-es only the three dynamic cursor
// properties (Last UUID, Message Count, Last Synced) on an existing claude-log
// page. Partial PATCH: only the 3 dynamic cursor props are sent so we don't
// clobber Title/SessionID/StartedAt/Project/SourcePath set at create time.
func (c *Client) UpdateClaudeLogCursorProperties(ctx context.Context, pageID, lastUUID string, messageCount int, lastSynced time.Time) error {
	props := map[string]any{
		"Last UUID": map[string]any{
			"rich_text": []map[string]any{
				{"type": "text", "text": map[string]any{"content": lastUUID}},
			},
		},
		"Message Count": map[string]any{"number": messageCount},
		"Last Synced": map[string]any{
			"date": map[string]any{"start": lastSynced.UTC().Format(time.RFC3339)},
		},
	}
	body := map[string]any{"properties": props}
	return c.do(ctx, "PATCH", "/pages/"+pageID, body, nil)
}

// CreatePageClaudeLog creates a new Notion page in the supplied claude-log
// database. Unlike CreatePage, the parent database ID is taken from the
// argument (not the client's configured DB) so callers can route writes to a
// dedicated claude-log DB without re-initialising the client.
func (c *Client) CreatePageClaudeLog(ctx context.Context, dbID string, props ClaudeLogProperties, blocks []map[string]any) (string, error) {
	parent, err := c.dbParent(ctx, dbID)
	if err != nil {
		return "", err
	}
	chunks := ChunkBlocks(blocks)
	var first []map[string]any
	if len(chunks) > 0 {
		first = chunks[0]
	}
	body := map[string]any{
		"parent":     parent,
		"properties": props.toMap(),
		"children":   first,
	}
	var resp Page
	if err := c.do(ctx, "POST", "/pages", body, &resp); err != nil {
		return "", err
	}
	if len(chunks) > 1 {
		for _, chunk := range chunks[1:] {
			if err := c.AppendChildren(ctx, resp.ID, chunk); err != nil {
				return resp.ID, err
			}
		}
	}
	return resp.ID, nil
}
