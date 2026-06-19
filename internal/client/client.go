// Package client is a thin typed HTTP client for the Revolut Merchant API.
//
// It is the one place that knows how to *talk* to Revolut: base-URL selection
// (sandbox vs production), the Authorization / Revolut-Api-Version headers, and
// turning non-2xx responses into a typed error. It mirrors the proven Python
// transport client in revolut-merchant-mcp so the auth/version/error shape is
// identical across the two codebases.
//
// It deliberately knows nothing about plans or webhooks — plan.go and webhook.go
// build typed calls on top of do().
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Public Merchant API hosts. Sandbox and production are entirely separate
// accounts with separate keys (a sandbox key will 401 against production).
const (
	SandboxBaseURL    = "https://sandbox-merchant.revolut.com/api"
	ProductionBaseURL = "https://merchant.revolut.com/api"

	// DefaultAPIVersion is the Revolut-Api-Version pinned by default. Webhooks
	// require >= 2024-09-01.
	DefaultAPIVersion = "2024-09-01"
)

// ErrNotFound is the sentinel returned for a 404 response. Resource Read methods
// match it (errors.Is) to drive resp.State.RemoveResource (drift handling).
var ErrNotFound = errors.New("revolut: resource not found")

// APIError wraps a non-2xx Revolut response (other than 404). Code/Message are
// pulled from Revolut's JSON error envelope when present.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Body       []byte
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("revolut API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("revolut API error %d: %s", e.StatusCode, e.Message)
}

// Client is a typed Revolut Merchant API HTTP client.
type Client struct {
	baseURL    string
	token      string
	apiVersion string
	httpClient *http.Client
}

// New constructs a Client. endpoint is the API base URL (no trailing slash
// required), token is the Merchant secret key, apiVersion is the
// Revolut-Api-Version header value (YYYY-MM-DD). apiVersion falls back to
// DefaultAPIVersion when empty.
func New(endpoint, token, apiVersion string) *Client {
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}
	return &Client{
		baseURL:    endpoint,
		token:      token,
		apiVersion: apiVersion,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL returns the configured API base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// do issues an authenticated request. body (if non-nil) is JSON-encoded; out (if
// non-nil) is JSON-decoded from a 2xx response. A 404 yields ErrNotFound; any
// other non-2xx yields an *APIError.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("revolut: marshal request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("revolut: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Revolut-Api-Version", c.apiVersion)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("revolut: transport error: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errorFor(resp.StatusCode, respBody)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("revolut: decode response: %w", err)
		}
	}
	return nil
}

// errorFor decodes Revolut's error envelope into an *APIError.
func errorFor(status int, body []byte) error {
	apiErr := &APIError{StatusCode: status, Body: body, Message: fmt.Sprintf("HTTP %d", status)}

	var env struct {
		Code        string `json:"code"`
		Error       string `json:"error"`
		Message     string `json:"message"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		if env.Code != "" {
			apiErr.Code = env.Code
		} else if env.Error != "" {
			apiErr.Code = env.Error
		}
		if env.Message != "" {
			apiErr.Message = env.Message
		} else if env.Description != "" {
			apiErr.Message = env.Description
		}
	}
	return apiErr
}
