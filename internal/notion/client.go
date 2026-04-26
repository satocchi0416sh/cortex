package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const (
	apiBase       = "https://api.notion.com/v1"
	notionVersion = "2026-03-11"
	defaultBurst  = 3
	maxRetries    = 4
)

type Client struct {
	http    *http.Client
	token   string
	dbID    string
	limiter *rate.Limiter
	logger  *slog.Logger
}

type Options struct {
	Token      string
	DatabaseID string
	RPS        float64
	Logger     *slog.Logger
	HTTPClient *http.Client
}

func NewClient(opts Options) *Client {
	if opts.RPS <= 0 {
		opts.RPS = 2.5
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	httpClient.Transport = &headerTransport{
		base:    transportOr(httpClient.Transport),
		token:   opts.Token,
		version: notionVersion,
	}
	return &Client{
		http:    httpClient,
		token:   opts.Token,
		dbID:    opts.DatabaseID,
		limiter: rate.NewLimiter(rate.Limit(opts.RPS), defaultBurst),
		logger:  opts.Logger,
	}
}

func (c *Client) DatabaseID() string { return c.dbID }

type headerTransport struct {
	base    http.RoundTripper
	token   string
	version string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("Notion-Version", t.version)
	if req.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return t.base.RoundTrip(req)
}

func transportOr(t http.RoundTripper) http.RoundTripper {
	if t != nil {
		return t
	}
	return http.DefaultTransport
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, apiBase+path, cloneReader(reqBody))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("http %s %s: %w", method, path, err)
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read body: %w", readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt >= maxRetries {
				return fmt.Errorf("notion %s %s: %d after %d retries: %s", method, path, resp.StatusCode, attempt, truncate(data))
			}
			wait := backoff(resp, attempt)
			c.logger.Warn("notion retry", "method", method, "path", path, "status", resp.StatusCode, "wait", wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("notion %s %s: %d %s", method, path, resp.StatusCode, truncate(data))
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
		}
		return nil
	}
}

func cloneReader(r io.Reader) io.Reader {
	if r == nil {
		return nil
	}
	br, ok := r.(*bytes.Reader)
	if !ok {
		return r
	}
	return bytes.NewReader(snapshot(br))
}

func snapshot(r *bytes.Reader) []byte {
	r.Seek(0, io.SeekStart)
	buf := make([]byte, r.Len())
	r.Read(buf)
	r.Seek(0, io.SeekStart)
	return buf
}

func backoff(resp *http.Response, attempt int) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

func truncate(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
