package notion

import (
	"context"
	"fmt"
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
	body := map[string]any{
		"filter": map[string]any{
			"property": "External ID",
			"rich_text": map[string]any{
				"equals": externalID,
			},
		},
		"page_size": 1,
	}
	var resp queryResult
	if err := c.do(ctx, "POST", fmt.Sprintf("/databases/%s/query", c.dbID), body, &resp); err != nil {
		return "", err
	}
	if len(resp.Results) == 0 {
		return "", nil
	}
	return resp.Results[0].ID, nil
}

func (c *Client) CreatePage(ctx context.Context, props Properties, blocks []map[string]any) (string, error) {
	chunks := ChunkBlocks(blocks)
	var first []map[string]any
	if len(chunks) > 0 {
		first = chunks[0]
	}
	body := map[string]any{
		"parent":     map[string]any{"database_id": c.dbID},
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

