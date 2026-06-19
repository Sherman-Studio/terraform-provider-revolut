package client

import (
	"context"
	"net/url"
)

// Webhook is a Revolut Merchant webhook subscription.
type Webhook struct {
	ID            string   `json:"id,omitempty"`
	URL           string   `json:"url"`
	Events        []string `json:"events"`
	SigningSecret string   `json:"signing_secret,omitempty"`
}

// CreateWebhookRequest is the POST /webhooks body. Requires Revolut-Api-Version
// >= 2024-09-01. A merchant may have at most 10 webhooks (422 on the 11th).
type CreateWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// UpdateWebhookRequest is the PATCH /webhooks/{id} body (partial update).
type UpdateWebhookRequest struct {
	URL    *string  `json:"url,omitempty"`
	Events []string `json:"events,omitempty"`
}

// RotateWebhookSecretRequest is the POST /webhooks/{id}/rotate-signing-secret body.
type RotateWebhookSecretRequest struct {
	// ExpirationPeriod is an ISO-8601 grace window for the old secret (e.g. PT5H30M).
	ExpirationPeriod *string `json:"expiration_period,omitempty"`
}

// CreateWebhook creates a webhook. The response includes signing_secret.
func (c *Client) CreateWebhook(ctx context.Context, req CreateWebhookRequest) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, "POST", "/webhooks", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWebhook fetches a webhook by id. This is the only Read path that returns
// signing_secret (the list endpoint omits it). A 404 yields ErrNotFound.
func (c *Client) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, "GET", "/webhooks/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWebhook patches url and/or events on a webhook.
func (c *Client) UpdateWebhook(ctx context.Context, id string, req UpdateWebhookRequest) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, "PATCH", "/webhooks/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWebhook deletes a webhook (204 on success). A 404 is treated as success
// by the caller (already gone).
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/webhooks/"+url.PathEscape(id), nil, nil)
}

// RotateWebhookSecret rotates the signing secret. The response includes the new
// signing_secret.
func (c *Client) RotateWebhookSecret(ctx context.Context, id string, req RotateWebhookSecretRequest) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, "POST", "/webhooks/"+url.PathEscape(id)+"/rotate-signing-secret", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWebhooks returns all webhooks. Items omit signing_secret.
func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out []Webhook
	if err := c.do(ctx, "GET", "/webhooks", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
