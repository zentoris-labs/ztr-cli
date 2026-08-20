// Package api is a thin HTTP client for the Zentoris main API: it attaches the resolved
// bearer credential, sets a stable User-Agent, threads If-Match for optimistic concurrency,
// and renders RFC 9457 problem+json responses as readable errors.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zentoris-labs/ztr-cli/internal/auth"
	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// Client calls the Zentoris main API.
type Client struct {
	cfg      *config.Config
	resolver *auth.Resolver
	ua       string
}

// New builds a client. cfg is read lazily on each call so late-parsed flags take effect.
func New(cfg *config.Config, resolver *auth.Resolver, version string) *Client {
	return &Client{
		cfg:      cfg,
		resolver: resolver,
		ua:       "zentoris/" + version,
	}
}

// Do sends a JSON request and decodes a JSON response into out (out may be nil). ifMatch,
// when non-empty, sets the If-Match header required by Zentoris PATCH endpoints.
func (c *Client) Do(ctx context.Context, method, path string, body any, ifMatch string, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}

	base := strings.TrimRight(c.cfg.APIBase, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return err
	}

	tok, _, err := c.resolver.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.ua)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}

	resp, err := c.cfg.HTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return parseProblem(resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// parseProblem renders an RFC 9457 problem+json body into a readable error, falling back to
// the raw status and body when the response is not problem-shaped.
func parseProblem(status int, data []byte) error {
	var p struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(data, &p) == nil && (p.Title != "" || p.Type != "") {
		msg := p.Title
		if p.Detail != "" {
			if msg != "" {
				msg += ": "
			}
			msg += p.Detail
		}
		if p.Type != "" {
			msg += " (" + p.Type + ")"
		}
		return fmt.Errorf("api %d: %s", status, msg)
	}
	return fmt.Errorf("api %d: %s", status, strings.TrimSpace(string(data)))
}
