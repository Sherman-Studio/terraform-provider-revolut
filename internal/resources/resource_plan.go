package resources

import (
	"context"
	"errors"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Interface assertions.
var (
	_ resource.Resource                = (*planResource)(nil)
	_ resource.ResourceWithConfigure   = (*planResource)(nil)
	_ resource.ResourceWithImportState = (*planResource)(nil)
)

// NewPlanResource is the revolut_plan resource constructor.
func NewPlanResource() resource.Resource {
	return &planResource{}
}

type planResource struct {
	client *client.Client
}

// planResourceModel is the top-level state model for revolut_plan.
//
// The full variations[] -> phases[] -> subscription_items[] tree is modeled with
// framework list-nested types. The whole tree is immutable (RequiresReplace):
// the Revolut Merchant API has no plan update endpoint.
type planResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	TrialDuration types.String `tfsdk:"trial_duration"`
	State         types.String `tfsdk:"state"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
	Variations    types.List   `tfsdk:"variations"`
}

// ---- Object attribute types for the nested tree -------------------------------

func planSubscriptionItemAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":     types.StringType,
		"amount":   types.Int64Type,
		"quantity": types.Int64Type,
	}
}

func planPhaseAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":             types.StringType,
		"ordinal":        types.Int64Type,
		"cycle_duration": types.StringType,
		"cycle_count":    types.Int64Type,
		"amount":         types.Int64Type,
		"currency":       types.StringType,
		"subscription_items": types.ListType{
			ElemType: types.ObjectType{AttrTypes: planSubscriptionItemAttrTypes()},
		},
	}
}

func planVariationAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":             types.StringType,
		"trial_duration": types.StringType,
		"phases": types.ListType{
			ElemType: types.ObjectType{AttrTypes: planPhaseAttrTypes()},
		},
	}
}

func (r *planResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plan"
}

func (r *planResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Revolut subscription plan. Create-and-replace only: the Merchant API has no plan update or delete endpoint, so every attribute forces replacement and `terraform destroy` only removes the plan from state (it is ORPHANED in Revolut).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Server-assigned plan UUID.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Plan name. Immutable (forces replacement).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"trial_duration": schema.StringAttribute{
				Description: "ISO-8601 trial duration (e.g. P14D). Immutable (forces replacement).",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"state": schema.StringAttribute{
				Description: "Plan lifecycle state (e.g. active).",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},
			"variations": schema.ListNestedAttribute{
				Description: "Pricing options for the plan. The entire variations tree is immutable: any change forces replacement of the whole plan.",
				Required:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Server-assigned variation UUID (referenceable when creating a subscription).",
							Computed:    true,
						},
						"trial_duration": schema.StringAttribute{
							Description: "ISO-8601 trial duration for this variation.",
							Optional:    true,
						},
						"phases": schema.ListNestedAttribute{
							Description: "Billing stages of this variation.",
							Required:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"id": schema.StringAttribute{
										Description: "Server-assigned phase UUID.",
										Computed:    true,
									},
									"ordinal": schema.Int64Attribute{
										Description: "1-based sequence position of this phase.",
										Required:    true,
									},
									"cycle_duration": schema.StringAttribute{
										Description: "ISO-8601 billing cycle duration (e.g. P1M, P1Y).",
										Required:    true,
									},
									"cycle_count": schema.Int64Attribute{
										Description: "Number of cycles this phase runs. Omit/null for indefinite.",
										Optional:    true,
									},
									"amount": schema.Int64Attribute{
										Description: "Charge amount in integer minor units (9900 = £99.00).",
										Optional:    true,
									},
									"currency": schema.StringAttribute{
										Description: "ISO 4217 currency code (e.g. GBP).",
										Optional:    true,
									},
									"subscription_items": schema.ListNestedAttribute{
										Description: "Line items billed within this phase.",
										Optional:    true,
										NestedObject: schema.NestedAttributeObject{
											Attributes: map[string]schema.Attribute{
												"type": schema.StringAttribute{
													Description: "Item type: flat or usage.",
													Required:    true,
												},
												"amount": schema.Int64Attribute{
													Description: "Item amount in integer minor units.",
													Optional:    true,
												},
												"quantity": schema.Int64Attribute{
													Description: "Item quantity.",
													Optional:    true,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *planResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *planResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan planResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiReq := client.CreatePlanRequest{
		Name:          plan.Name.ValueString(),
		TrialDuration: planOptString(plan.TrialDuration),
	}
	apiReq.Variations = planVariationsToAPI(ctx, plan.Variations, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreatePlan(ctx, apiReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating plan", err.Error())
		return
	}

	resp.Diagnostics.Append(planMapToState(ctx, created, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *planResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state planResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetPlan(ctx, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading plan", err.Error())
		return
	}

	resp.Diagnostics.Append(planMapToState(ctx, got, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *planResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// No-op: the plan has no update endpoint, so every attribute is
	// RequiresReplace and Update is never reached in practice. Guard anyway.
	resp.Diagnostics.AddError(
		"Plan update not supported",
		"The Revolut Merchant API has no plan update endpoint. Every attribute is marked RequiresReplace; reaching Update indicates a provider bug.",
	)
}

func (r *planResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	// No delete API. State is auto-cleared on return. The plan is ORPHANED in
	// Revolut — warn loudly.
	resp.Diagnostics.AddWarning(
		"Plan orphaned in Revolut",
		"The Revolut Merchant API has no plan delete endpoint. This plan has been removed from Terraform state but STILL EXISTS in your Revolut account. Archive or disable it from the Revolut dashboard if it should no longer be billed against.",
	)
}

func (r *planResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---- model <-> API mapping ----------------------------------------------------

// planOptString returns nil for a null/unknown string, else a pointer to its value.
func planOptString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// planOptInt64 returns nil for a null/unknown int, else a pointer to its value.
func planOptInt64(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	n := v.ValueInt64()
	return &n
}

// nestedModel mirrors the schema's nested object for ElementsAs decoding.
type variationModel struct {
	ID            types.String `tfsdk:"id"`
	TrialDuration types.String `tfsdk:"trial_duration"`
	Phases        types.List   `tfsdk:"phases"`
}

type phaseModel struct {
	ID                types.String `tfsdk:"id"`
	Ordinal           types.Int64  `tfsdk:"ordinal"`
	CycleDuration     types.String `tfsdk:"cycle_duration"`
	CycleCount        types.Int64  `tfsdk:"cycle_count"`
	Amount            types.Int64  `tfsdk:"amount"`
	Currency          types.String `tfsdk:"currency"`
	SubscriptionItems types.List   `tfsdk:"subscription_items"`
}

type subscriptionItemModel struct {
	Type     types.String `tfsdk:"type"`
	Amount   types.Int64  `tfsdk:"amount"`
	Quantity types.Int64  `tfsdk:"quantity"`
}

// planVariationsToAPI decodes the config variations list into client structs.
func planVariationsToAPI(ctx context.Context, list types.List, diags *diag.Diagnostics) []client.Variation {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var vms []variationModel
	diags.Append(list.ElementsAs(ctx, &vms, false)...)
	if diags.HasError() {
		return nil
	}

	out := make([]client.Variation, 0, len(vms))
	for _, vm := range vms {
		v := client.Variation{
			TrialDuration: planOptString(vm.TrialDuration),
		}

		var pms []phaseModel
		diags.Append(vm.Phases.ElementsAs(ctx, &pms, false)...)
		if diags.HasError() {
			return nil
		}
		v.Phases = make([]client.Phase, 0, len(pms))
		for _, pm := range pms {
			p := client.Phase{
				Ordinal:       pm.Ordinal.ValueInt64(),
				CycleDuration: pm.CycleDuration.ValueString(),
				CycleCount:    planOptInt64(pm.CycleCount),
				Amount:        planOptInt64(pm.Amount),
				Currency:      planOptString(pm.Currency),
			}
			if !pm.SubscriptionItems.IsNull() && !pm.SubscriptionItems.IsUnknown() {
				var sims []subscriptionItemModel
				diags.Append(pm.SubscriptionItems.ElementsAs(ctx, &sims, false)...)
				if diags.HasError() {
					return nil
				}
				p.SubscriptionItems = make([]client.SubscriptionItem, 0, len(sims))
				for _, sim := range sims {
					p.SubscriptionItems = append(p.SubscriptionItems, client.SubscriptionItem{
						Type:     sim.Type.ValueString(),
						Amount:   planOptInt64(sim.Amount),
						Quantity: planOptInt64(sim.Quantity),
					})
				}
			}
			v.Phases = append(v.Phases, p)
		}
		out = append(out, v)
	}
	return out
}

// planMapToState maps a server Plan onto a framework model (resource or shared).
func planMapToState(ctx context.Context, p *client.Plan, m *planResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.TrialDuration = planPtrToString(p.TrialDuration)
	m.State = planStringOrNull(p.State)
	m.CreatedAt = planStringOrNull(p.CreatedAt)
	m.UpdatedAt = planStringOrNull(p.UpdatedAt)

	vlist, d := planVariationsToList(ctx, p.Variations)
	diags.Append(d...)
	m.Variations = vlist
	return diags
}

// planVariationsToList builds the framework list value from server variations.
func planVariationsToList(ctx context.Context, vars []client.Variation) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	objs := make([]attr.Value, 0, len(vars))
	for _, v := range vars {
		plist, d := planPhasesToList(ctx, v.Phases)
		diags.Append(d...)

		obj, d := types.ObjectValue(planVariationAttrTypes(), map[string]attr.Value{
			"id":             planStringOrNull(v.ID),
			"trial_duration": planPtrToString(v.TrialDuration),
			"phases":         plist,
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: planVariationAttrTypes()}, objs)
	diags.Append(d...)
	return list, diags
}

func planPhasesToList(ctx context.Context, phases []client.Phase) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	objs := make([]attr.Value, 0, len(phases))
	for _, p := range phases {
		ilist, d := planItemsToList(p.SubscriptionItems)
		diags.Append(d...)

		obj, d := types.ObjectValue(planPhaseAttrTypes(), map[string]attr.Value{
			"id":                 planStringOrNull(p.ID),
			"ordinal":            types.Int64Value(p.Ordinal),
			"cycle_duration":     types.StringValue(p.CycleDuration),
			"cycle_count":        planPtrToInt64(p.CycleCount),
			"amount":             planPtrToInt64(p.Amount),
			"currency":           planPtrToString(p.Currency),
			"subscription_items": ilist,
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: planPhaseAttrTypes()}, objs)
	diags.Append(d...)
	return list, diags
}

func planItemsToList(items []client.SubscriptionItem) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	itemType := types.ObjectType{AttrTypes: planSubscriptionItemAttrTypes()}
	// Preserve null vs empty: an absent items slice becomes a null list so it
	// round-trips against an Optional attribute the user omitted.
	if len(items) == 0 {
		return types.ListNull(itemType), diags
	}
	objs := make([]attr.Value, 0, len(items))
	for _, it := range items {
		obj, d := types.ObjectValue(planSubscriptionItemAttrTypes(), map[string]attr.Value{
			"type":     types.StringValue(it.Type),
			"amount":   planPtrToInt64(it.Amount),
			"quantity": planPtrToInt64(it.Quantity),
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}
	list, d := types.ListValue(itemType, objs)
	diags.Append(d...)
	return list, diags
}

// ---- scalar helpers -----------------------------------------------------------

func planPtrToString(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func planPtrToInt64(p *int64) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

// planStringOrNull maps an empty server string (omitempty) to a null value so
// computed attributes don't flip between "" and null.
func planStringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
