package resources

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	_ resource.Resource                = (*planVariationResource)(nil)
	_ resource.ResourceWithConfigure   = (*planVariationResource)(nil)
	_ resource.ResourceWithImportState = (*planVariationResource)(nil)
)

// NewPlanVariationResource is the revolut_plan_variation resource constructor.
//
// The Revolut Merchant API has NO independent variation endpoints — variations
// are created inline in the plan-create body (POST /subscription-plans) and are
// immutable along with the plan. This resource is a thin, read-through
// projection over the parent plan: it never POSTs/PATCHes/DELETEs a variation.
// "Create" resolves the variation out of GET /subscription-plans/{plan_id};
// every field is effectively ForceNew via the parent plan, and Delete only drops
// the row from Terraform state (the variation is ORPHANED in Revolut).
func NewPlanVariationResource() resource.Resource {
	return &planVariationResource{}
}

type planVariationResource struct {
	client *client.Client
}

type planVariationResourceModel struct {
	ID            types.String `tfsdk:"id"`
	PlanID        types.String `tfsdk:"plan_id"`
	TrialDuration types.String `tfsdk:"trial_duration"`
	Phases        types.List   `tfsdk:"phases"`
}

// planVariationSubscriptionItemAttrTypes / planVariationPhaseAttrTypes describe the nested object element
// types. They are the single source of truth used by both the schema and the
// model<->state conversions below (shared with the data source).
func planVariationSubscriptionItemAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":     types.StringType,
		"amount":   types.Int64Type,
		"quantity": types.Int64Type,
	}
}

func planVariationPhaseAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":             types.StringType,
		"ordinal":        types.Int64Type,
		"cycle_duration": types.StringType,
		"cycle_count":    types.Int64Type,
		"amount":         types.Int64Type,
		"currency":       types.StringType,
		"subscription_items": types.ListType{
			ElemType: types.ObjectType{AttrTypes: planVariationSubscriptionItemAttrTypes()},
		},
	}
}

func (r *planVariationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plan_variation"
}

func (r *planVariationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A pricing variation (per-currency / per-interval variant) of a Revolut subscription plan. " +
			"Variations have NO independent API: they are created inline with the plan (POST /subscription-plans) " +
			"and are immutable. This resource is a read-through projection over the parent plan — it never mutates " +
			"the variation. Every attribute forces replacement, and `terraform destroy` only removes it from state " +
			"(the variation remains ORPHANED in Revolut). The computed `id` is usable as a reference for runtime " +
			"create-subscription calls.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Server-assigned variation UUID. When set in config it selects a specific variation " +
					"inside the parent plan; when omitted, the plan's single variation is adopted. Immutable.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan_id": schema.StringAttribute{
				Description: "UUID of the parent plan. Immutable.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"trial_duration": schema.StringAttribute{
				Description: "ISO-8601 trial duration for this variation (e.g. P14D). Server-assigned; immutable.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"phases": schema.ListNestedAttribute{
				Description: "Ordered billing phases of this variation. Server-assigned; immutable.",
				Computed:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{Description: "Server-assigned phase UUID.", Computed: true},
						"ordinal":        schema.Int64Attribute{Description: "Phase order (0-based).", Computed: true},
						"cycle_duration": schema.StringAttribute{Description: "ISO-8601 billing cycle duration (e.g. P1M).", Computed: true},
						"cycle_count":    schema.Int64Attribute{Description: "Number of cycles before advancing; null = open-ended.", Computed: true},
						"amount":         schema.Int64Attribute{Description: "Phase amount in minor units.", Computed: true},
						"currency":       schema.StringAttribute{Description: "ISO-4217 currency code.", Computed: true},
						"subscription_items": schema.ListNestedAttribute{
							Description: "Line items billed in this phase.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"type":     schema.StringAttribute{Description: "Item type (flat | usage).", Computed: true},
									"amount":   schema.Int64Attribute{Description: "Item amount in minor units.", Computed: true},
									"quantity": schema.Int64Attribute{Description: "Item quantity.", Computed: true},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *planVariationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

// Create resolves the variation out of the parent plan. Variations have no
// create endpoint, so this is a pure read of GET /subscription-plans/{plan_id}.
func (r *planVariationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data planVariationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan, err := r.client.GetPlan(ctx, data.PlanID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.Diagnostics.AddError(
				"Parent plan not found",
				fmt.Sprintf("Plan %q does not exist; cannot resolve a variation from it.", data.PlanID.ValueString()),
			)
			return
		}
		resp.Diagnostics.AddError("Reading parent plan failed", err.Error())
		return
	}

	variation, diags := selectVariation(plan, data.ID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(applyVariationToModel(ctx, &data, variation)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *planVariationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data planVariationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan, err := r.client.GetPlan(ctx, data.PlanID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading parent plan failed", err.Error())
		return
	}

	variation := findVariation(plan, data.ID.ValueString())
	if variation == nil {
		// The variation vanished from the plan (or the plan was rebuilt) — drift.
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(applyVariationToModel(ctx, &data, variation)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is unreachable in practice: every attribute is RequiresReplace and
// variations are immutable. It persists the planned values to honor the contract.
func (r *planVariationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data planVariationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *planVariationResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	// No delete API for variations. State is auto-cleared on return; warn that
	// the variation still lives inside the plan in Revolut.
	resp.Diagnostics.AddWarning(
		"Plan variation orphaned in Revolut",
		"The Revolut Merchant API has no endpoint to delete a plan variation. This variation has been removed "+
			"from Terraform state but still exists inside its parent plan in your Revolut account.",
	)
}

// ImportState accepts a "plan_id:variation_id" composite id.
func (r *planVariationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected import ID in the form \"plan_id:variation_id\", got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("plan_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

// ---- shared helpers (used by the data source too) ----

// findVariation returns the variation with the given id, or nil.
func findVariation(plan *client.Plan, id string) *client.Variation {
	for i := range plan.Variations {
		if plan.Variations[i].ID == id {
			return &plan.Variations[i]
		}
	}
	return nil
}

// selectVariation picks the variation to bind to. When id is known it must
// match; when id is null/unknown the plan must have exactly one variation.
func selectVariation(plan *client.Plan, id types.String) (*client.Variation, diag.Diagnostics) {
	var diags diag.Diagnostics
	if !id.IsNull() && !id.IsUnknown() && id.ValueString() != "" {
		v := findVariation(plan, id.ValueString())
		if v == nil {
			diags.AddError(
				"Variation not found",
				fmt.Sprintf("Plan %q has no variation with id %q.", plan.ID, id.ValueString()),
			)
		}
		return v, diags
	}
	switch len(plan.Variations) {
	case 0:
		diags.AddError("No variations in plan", fmt.Sprintf("Plan %q has no variations to bind to.", plan.ID))
		return nil, diags
	case 1:
		return &plan.Variations[0], diags
	default:
		diags.AddError(
			"Ambiguous variation",
			fmt.Sprintf("Plan %q has %d variations; set `id` to choose one.", plan.ID, len(plan.Variations)),
		)
		return nil, diags
	}
}

// applyVariationToModel maps a client.Variation into the resource model.
func applyVariationToModel(ctx context.Context, m *planVariationResourceModel, v *client.Variation) diag.Diagnostics {
	m.ID = types.StringValue(v.ID)
	m.TrialDuration = planVariationPtrToString(v.TrialDuration)
	phases, diags := planVariationPhasesToList(ctx, v.Phases)
	m.Phases = phases
	return diags
}

// planVariationPtrToString converts an optional API string into a framework value.
func planVariationPtrToString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// planVariationPtrToInt64 converts an optional API int64 into a framework value.
func planVariationPtrToInt64(i *int64) types.Int64 {
	if i == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*i)
}

// planVariationPhasesToList converts client phases into a framework list of phase objects.
func planVariationPhasesToList(ctx context.Context, phases []client.Phase) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	phaseObjType := types.ObjectType{AttrTypes: planVariationPhaseAttrTypes()}

	objs := make([]attr.Value, 0, len(phases))
	for i := range phases {
		p := phases[i]

		items := make([]attr.Value, 0, len(p.SubscriptionItems))
		for j := range p.SubscriptionItems {
			it := p.SubscriptionItems[j]
			obj, d := types.ObjectValue(planVariationSubscriptionItemAttrTypes(), map[string]attr.Value{
				"type":     types.StringValue(it.Type),
				"amount":   planVariationPtrToInt64(it.Amount),
				"quantity": planVariationPtrToInt64(it.Quantity),
			})
			diags.Append(d...)
			items = append(items, obj)
		}
		itemsList, d := types.ListValue(types.ObjectType{AttrTypes: planVariationSubscriptionItemAttrTypes()}, items)
		diags.Append(d...)

		obj, d := types.ObjectValue(planVariationPhaseAttrTypes(), map[string]attr.Value{
			"id":                 types.StringValue(p.ID),
			"ordinal":            types.Int64Value(p.Ordinal),
			"cycle_duration":     types.StringValue(p.CycleDuration),
			"cycle_count":        planVariationPtrToInt64(p.CycleCount),
			"amount":             planVariationPtrToInt64(p.Amount),
			"currency":           planVariationPtrToString(p.Currency),
			"subscription_items": itemsList,
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}

	list, d := types.ListValue(phaseObjType, objs)
	diags.Append(d...)
	return list, diags
}
