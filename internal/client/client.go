// Package client is a minimal HTTP client for the Orch8 REST API (/api/v1).
package client

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

// DefaultEndpoint is used when neither the provider block nor ORCH8_URL sets one.
const DefaultEndpoint = "http://127.0.0.1:8080"

// APIPrefix is the canonical versioned mount of the engine's REST API.
const APIPrefix = "/api/v1"

// Client talks to one Orch8 engine.
type Client struct {
	BaseURL  string // e.g. http://127.0.0.1:8080/api/v1
	APIKey   string
	TenantID string
	HTTP     *http.Client
	// UserAgent is sent on every request.
	UserAgent string
}

// New builds a client for endpoint (scheme://host[:port], with or without a
// trailing /api/v1).
func New(endpoint, apiKey, tenantID string) (*Client, error) {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid Orch8 endpoint %q: must be an absolute http(s) URL", endpoint)
	}
	base := strings.TrimRight(endpoint, "/")
	if !strings.HasSuffix(base, APIPrefix) {
		base += APIPrefix
	}
	return &Client{
		BaseURL:   base,
		APIKey:    apiKey,
		TenantID:  tenantID,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		UserAgent: "terraform-provider-orch8",
	}, nil
}

// APIError is a non-2xx response from the engine.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("orch8 API %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, strings.TrimSpace(e.Body))
}

// IsNotFound reports whether err is a 404 from the engine.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// MaxRetries bounds retries of 429/503 responses (e.g. SQLite "database is
// locked" surfaces as 503 while Terraform runs operations in parallel).
var MaxRetries = 5

// RetryBaseDelay is the first backoff delay; it doubles per attempt.
var RetryBaseDelay = 200 * time.Millisecond

// Do issues a request. body (if non-nil) is JSON-encoded; out (if non-nil)
// receives the decoded JSON response. 429 and 503 responses are retried with
// exponential backoff: the engine returns them before committing any change.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var buf []byte
	if body != nil {
		var err error
		if buf, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
	}
	delay := RetryBaseDelay
	for attempt := 0; ; attempt++ {
		err := c.do(ctx, method, path, query, buf, body != nil, out)
		var apiErr *APIError
		if attempt >= MaxRetries || !errors.As(err, &apiErr) ||
			(apiErr.StatusCode != http.StatusServiceUnavailable && apiErr.StatusCode != http.StatusTooManyRequests) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(delay):
		}
		delay *= 2
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, buf []byte, hasBody bool, out any) error {
	full := c.BaseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var reader io.Reader
	if hasBody {
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}
	if c.TenantID != "" {
		req.Header.Set("x-tenant-id", c.TenantID)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("orch8 API %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(data)}
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response of %s %s: %w", method, path, err)
		}
	}
	return nil
}

// PathEscape escapes one path segment.
func PathEscape(s string) string { return url.PathEscape(s) }
