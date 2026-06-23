package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// Plan-time validators for the revolut_webhook resource. Both target the
// silent-misconfiguration class: a webhook that applies cleanly but doesn't do
// what you meant, surfacing only later as "events never arrive" in production.

// --- url: must be HTTPS ---------------------------------------------------

type httpsURLValidator struct{}

func (httpsURLValidator) Description(_ context.Context) string {
	return "value must be an https:// URL"
}

func (v httpsURLValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (httpsURLValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if !strings.HasPrefix(strings.ToLower(s), "https://") {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Webhook URL must use HTTPS",
			fmt.Sprintf("Revolut only delivers webhooks to https:// endpoints; got %q.", s),
		)
	}
}

// httpsURL validates that a string attribute is an https:// URL.
func httpsURL() validator.String { return httpsURLValidator{} }

// --- events: warn on values outside the documented catalogue --------------

// knownWebhookEvents is the documented Revolut Merchant webhook catalogue
// (developer.revolut.com/docs/merchant/webhooks). A value outside this set is
// almost always a typo (e.g. ORDER_COMPLETE vs ORDER_COMPLETED) — precisely the
// silent misconfig that leaves a webhook registered but not delivering the
// event you expect. We WARN rather than error so a genuinely-new Revolut event
// isn't hard-blocked before this list catches up; the warning is loud in plan
// output, which is enough to catch a fat-finger.
var knownWebhookEvents = map[string]struct{}{
	"ORDER_AUTHORISED":            {},
	"ORDER_COMPLETED":             {},
	"ORDER_CANCELLED":             {},
	"ORDER_PAYMENT_AUTHENTICATED": {},
	"ORDER_PAYMENT_DECLINED":      {},
	"ORDER_PAYMENT_FAILED":        {},
	"ORDER_FAILED":                {},
	"SUBSCRIPTION_INITIATED":      {},
	"SUBSCRIPTION_OVERDUE":        {},
	"SUBSCRIPTION_CANCELLED":      {},
	"SUBSCRIPTION_FINISHED":       {},
	"PAYOUT_INITIATED":            {},
	"PAYOUT_COMPLETED":            {},
	"PAYOUT_FAILED":               {},
}

func knownEventsList() string {
	out := make([]string, 0, len(knownWebhookEvents))
	for k := range knownWebhookEvents {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

type knownEventsValidator struct{}

func (knownEventsValidator) Description(_ context.Context) string {
	return "warns when an event is not in the documented Revolut webhook catalogue"
}

func (v knownEventsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (knownEventsValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var events []string
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &events, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, e := range events {
		if _, ok := knownWebhookEvents[e]; !ok {
			resp.Diagnostics.AddAttributeWarning(
				req.Path,
				"Unrecognised Revolut webhook event",
				fmt.Sprintf(
					"%q is not in the known Revolut webhook catalogue. If this is a typo, "+
						"the webhook will silently not deliver the event you expect. Known events: %s",
					e, knownEventsList(),
				),
			)
		}
	}
}

// knownEvents warns when an events Set contains a value outside the documented
// Revolut catalogue (typo guard; non-blocking).
func knownEvents() validator.Set { return knownEventsValidator{} }
