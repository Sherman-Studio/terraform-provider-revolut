package client

import (
	"context"
	"net/url"
)

// SubscriptionItem is a line item within a plan phase.
//
// The live Revolut API (sandbox-validated 2026-06-19) requires every item to
// carry name, unit and type. A "flat" item additionally requires quantity; a
// "usage" item additionally requires code. amount/currency are echoed back on
// the item. (The phase-level amount/currency are only used when a phase has no
// subscription_items.)
type SubscriptionItem struct {
	ID       string  `json:"id,omitempty"`
	Name     string  `json:"name"`
	Unit     string  `json:"unit"`
	Type     string  `json:"type"` // flat | usage
	Code     *string `json:"code,omitempty"`
	Quantity *int64  `json:"quantity,omitempty"`
	Amount   *int64  `json:"amount,omitempty"`
	Currency *string `json:"currency,omitempty"`
}

// Phase is a billing stage within a plan variation.
type Phase struct {
	ID                string             `json:"id,omitempty"`
	Ordinal           int64              `json:"ordinal"`
	CycleDuration     string             `json:"cycle_duration"`
	CycleCount        *int64             `json:"cycle_count,omitempty"`
	Amount            *int64             `json:"amount,omitempty"`
	Currency          *string            `json:"currency,omitempty"`
	SubscriptionItems []SubscriptionItem `json:"subscription_items,omitempty"`
}

// Variation is a pricing option of a plan.
type Variation struct {
	ID            string  `json:"id,omitempty"`
	TrialDuration *string `json:"trial_duration,omitempty"`
	Phases        []Phase `json:"phases"`
}

// Plan is a Revolut subscription plan.
type Plan struct {
	ID            string      `json:"id,omitempty"`
	Name          string      `json:"name"`
	TrialDuration *string     `json:"trial_duration,omitempty"`
	State         string      `json:"state,omitempty"`
	CreatedAt     string      `json:"created_at,omitempty"`
	UpdatedAt     string      `json:"updated_at,omitempty"`
	Variations    []Variation `json:"variations"`
}

// CreatePlanRequest is the POST /subscription-plans body. Variations/phases/items
// are inline; the server assigns all UUIDs.
type CreatePlanRequest struct {
	Name          string      `json:"name"`
	TrialDuration *string     `json:"trial_duration,omitempty"`
	Variations    []Variation `json:"variations"`
}

// ListPlansResponse is the paginated GET /subscription-plans envelope. The live
// API returns the list under the "subscription_plans" key (sandbox-validated
// 2026-06-19), not "plans".
type ListPlansResponse struct {
	Plans         []Plan `json:"subscription_plans"`
	NextPageToken string `json:"next_page_token,omitempty"`
}

// CreatePlan creates a subscription plan. There is no update or delete endpoint.
func (c *Client) CreatePlan(ctx context.Context, req CreatePlanRequest) (*Plan, error) {
	var out Plan
	if err := c.do(ctx, "POST", "/subscription-plans", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPlan fetches a plan by id. A 404 yields ErrNotFound.
func (c *Client) GetPlan(ctx context.Context, id string) (*Plan, error) {
	var out Plan
	if err := c.do(ctx, "GET", "/subscription-plans/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPlans returns one page of plans. Pass an empty pageToken for the first page.
func (c *Client) ListPlans(ctx context.Context, pageToken string) (*ListPlansResponse, error) {
	path := "/subscription-plans"
	if pageToken != "" {
		path += "?page_token=" + url.QueryEscape(pageToken)
	}
	var out ListPlansResponse
	if err := c.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
