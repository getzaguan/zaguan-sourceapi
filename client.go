package sourceapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnauthorized means the hub rejected the source token (revoked or wrong).
var ErrUnauthorized = errors.New("sourceapi: source token rejected")

// APIError is a non-2xx answer from the hub.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("sourceapi: hub %d %s: %s", e.StatusCode, e.Code, e.Message)
	case e.Message != "":
		return fmt.Sprintf("sourceapi: hub %d: %s", e.StatusCode, e.Message)
	case e.Body != "":
		return fmt.Sprintf("sourceapi: hub %d: %s", e.StatusCode, e.Body)
	default:
		return fmt.Sprintf("sourceapi: hub %d", e.StatusCode)
	}
}

// Unwrap lets callers use errors.Is(err, ErrUnauthorized).
func (e *APIError) Unwrap() error {
	if e.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	return nil
}

// Config configures a Source API client.
type Config struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// Client talks to one hub source.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New validates cfg and returns a client.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("sourceapi: BaseURL is required")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("sourceapi: BaseURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("sourceapi: BaseURL scheme %q is not http(s)", u.Scheme)
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("sourceapi: source token is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		base:  strings.TrimRight(cfg.BaseURL, "/"),
		token: cfg.Token,
		http:  httpClient,
	}, nil
}

// Snapshot publishes a full snapshot.
func (c *Client) Snapshot(ctx context.Context, snap Snapshot) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return c.do(ctx, http.MethodPut, "/api/v1/source/snapshot", snap, nil)
}

// Events publishes a batch of deltas.
func (c *Client) Events(ctx context.Context, events Events) (EventsResponse, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var out EventsResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/source/events", events, &out)
	return out, err
}

// Heartbeat keeps the source lease alive.
func (c *Client) Heartbeat(ctx context.Context) (HeartbeatResponse, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var out HeartbeatResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/source/heartbeat", nil, &out)
	return out, err
}

// Commands long-polls the hub for commands. A 204 means no command.
func (c *Client) Commands(ctx context.Context, wait time.Duration) ([]Command, error) {
	if wait <= 0 {
		wait = 25 * time.Second
	}
	if wait > 60*time.Second {
		wait = 60 * time.Second
	}
	query := url.Values{}
	query.Set("wait", wait.String())
	path := "/api/v1/source/commands?" + query.Encode()
	var out []Command
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Result answers a command.
func (c *Client) Result(ctx context.Context, id string, result CommandResult) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return c.do(ctx, http.MethodPost, "/api/v1/source/commands/"+url.PathEscape(id), result, nil)
}

// Meta updates the source display metadata.
func (c *Client) Meta(ctx context.Context, meta Meta) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return c.do(ctx, http.MethodPut, "/api/v1/source", meta, nil)
}

// Delete revokes the source and removes its devices from the hub.
func (c *Client) Delete(ctx context.Context) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return c.do(ctx, http.MethodDelete, "/api/v1/source", nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(data)),
		}
		if len(data) > 0 {
			var payload struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(data, &payload) == nil {
				apiErr.Code = payload.Code
				apiErr.Message = payload.Message
			}
		}
		return apiErr
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("sourceapi: decode response: %w", err)
	}
	return nil
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 30*time.Second)
}

// String makes logs friendlier without exposing the token.
func (c *Client) String() string {
	return "sourceclient(" + c.base + ")"
}
