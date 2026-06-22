package resources

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Interface assertions.
var (
	_ resource.Resource                = (*webhookResource)(nil)
	_ resource.ResourceWithConfigure   = (*webhookResource)(nil)
	_ resource.ResourceWithImportState = (*webhookResource)(nil)
)

// NewWebhookResource is the revolut_webhook resource constructor.
func NewWebhookResource() resource.Resource {
	return &webhookResource{}
}

type webhookResource struct {
	client *client.Client
}

type webhookResourceModel struct {
	ID               types.String `tfsdk:"id"`
	URL              types.String `tfsdk:"url"`
	Events           types.Set    `tfsdk:"events"`
	SigningSecret    types.String `tfsdk:"signing_secret"`
	RotateTrigger    types.String `tfsdk:"rotate_trigger"`
	ExpirationPeriod types.String `tfsdk:"expiration_period"`
}

func (r *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Revolut Merchant webhook. url and events are mutable in place (PATCH). The signing_secret is server-generated and sensitive; changing rotate_trigger rotates it. Max 10 webhooks per merchant.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Server-assigned webhook UUID.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"url": schema.StringAttribute{
				Description: "HTTPS endpoint that receives webhook events. Mutable in place.",
				Required:    true,
				Validators: []validator.String{
					httpsURL(),
				},
			},
			"events": schema.SetAttribute{
				Description: "Event types to subscribe to (e.g. ORDER_COMPLETED). Mutable in place.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					knownEvents(),
				},
			},
			"signing_secret": schema.StringAttribute{
				Description: "Server-generated signing secret (wsk_...). Sensitive; never an input.",
				Computed:    true,
				Sensitive:   true,
			},
			"rotate_trigger": schema.StringAttribute{
				Description: "Arbitrary keeper value. Changing it triggers a signing-secret rotation.",
				Optional:    true,
			},
			"expiration_period": schema.StringAttribute{
				Description: "ISO-8601 grace window for the previous secret during rotation (e.g. PT5H30M).",
				Optional:    true,
			},
		},
	}
}

func (r *webhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

// eventsToStrings decodes the events Set into a []string for the client.
func eventsToStrings(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}
	out := make([]string, 0, len(set.Elements()))
	diags := set.ElementsAs(ctx, &out, false)
	return out, diags
}

// applyWebhook writes the server-returned webhook (id, url, events,
// signing_secret) into the model. Keeper/config-only fields (rotate_trigger,
// expiration_period) are left untouched for the caller to preserve.
func applyWebhook(ctx context.Context, m *webhookResourceModel, wh *client.Webhook) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(wh.ID)
	m.URL = types.StringValue(wh.URL)

	events, d := types.SetValueFrom(ctx, types.StringType, wh.Events)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	m.Events = events

	// signing_secret is only ever populated from server responses. Read-one,
	// create, and rotate return it; if absent, leave any prior value in place.
	if wh.SigningSecret != "" {
		m.SigningSecret = types.StringValue(wh.SigningSecret)
	}
	return diags
}

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	events, diags := eventsToStrings(ctx, plan.Events)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	wh, err := r.client.CreateWebhook(ctx, client.CreateWebhookRequest{
		URL:    plan.URL.ValueString(),
		Events: events,
	})
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
			resp.Diagnostics.AddError(
				"Could not create Revolut webhook",
				fmt.Sprintf("Revolut rejected the webhook (HTTP 422). A merchant may have at most 10 webhooks; delete an existing one or check the url/events payload. Detail: %s", apiErr.Error()),
			)
			return
		}
		resp.Diagnostics.AddError("Could not create Revolut webhook", err.Error())
		return
	}

	resp.Diagnostics.Append(applyWebhook(ctx, &plan, wh)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// signing_secret always comes back from POST; guarantee it is known.
	if plan.SigningSecret.IsUnknown() {
		plan.SigningSecret = types.StringValue(wh.SigningSecret)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GET /webhooks/{id} is the only read path that returns signing_secret; the
	// list endpoint omits it.
	wh, err := r.client.GetWebhook(ctx, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Could not read Revolut webhook", err.Error())
		return
	}

	resp.Diagnostics.Append(applyWebhook(ctx, &state, wh)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state webhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// PATCH url/events when either changed. Send both for a deterministic result.
	if !plan.URL.Equal(state.URL) || !plan.Events.Equal(state.Events) {
		events, diags := eventsToStrings(ctx, plan.Events)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		urlVal := plan.URL.ValueString()
		wh, err := r.client.UpdateWebhook(ctx, id, client.UpdateWebhookRequest{
			URL:    &urlVal,
			Events: events,
		})
		if err != nil {
			resp.Diagnostics.AddError("Could not update Revolut webhook", err.Error())
			return
		}
		resp.Diagnostics.Append(applyWebhook(ctx, &plan, wh)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// rotate_trigger change is the only way the signing_secret rotates.
	if !plan.RotateTrigger.Equal(state.RotateTrigger) {
		var expPeriod *string
		if !plan.ExpirationPeriod.IsNull() && !plan.ExpirationPeriod.IsUnknown() {
			v := plan.ExpirationPeriod.ValueString()
			expPeriod = &v
		}
		wh, err := r.client.RotateWebhookSecret(ctx, id, client.RotateWebhookSecretRequest{
			ExpirationPeriod: expPeriod,
		})
		if err != nil {
			resp.Diagnostics.AddError("Could not rotate Revolut webhook signing secret", err.Error())
			return
		}
		// rotate returns the new signing_secret (and current url/events).
		resp.Diagnostics.Append(applyWebhook(ctx, &plan, wh)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Preserve identity/output if no remote call repopulated them.
	if plan.ID.IsUnknown() || plan.ID.IsNull() {
		plan.ID = state.ID
	}
	if plan.SigningSecret.IsUnknown() || plan.SigningSecret.IsNull() {
		plan.SigningSecret = state.SigningSecret
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteWebhook(ctx, state.ID.ValueString()); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// Already gone — treat as success.
			return
		}
		resp.Diagnostics.AddError("Could not delete Revolut webhook", err.Error())
		return
	}
}

func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Passthrough the webhook UUID into id; Read recovers url/events/signing_secret.
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
