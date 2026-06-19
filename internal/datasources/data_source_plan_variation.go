package datasources

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*planVariationDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*planVariationDataSource)(nil)
)

// NewPlanVariationDataSource is the revolut_plan_variation data source constructor.
//
// It reads a single pricing variation out of its parent plan via
// GET /subscription-plans/{plan_id} and locates the variation by id. Variations
// have no standalone endpoint, so there is no per-variation GET.
func NewPlanVariationDataSource() datasource.DataSource {
	return &planVariationDataSource{}
}

type planVariationDataSource struct {
	client *client.Client
}

type planVariationDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	PlanID        types.String `tfsdk:"plan_id"`
	TrialDuration types.String `tfsdk:"trial_duration"`
	Phases        types.List   `tfsdk:"phases"`
}

func (d *planVariationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plan_variation"
}

func (d *planVariationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a single pricing variation within a Revolut subscription plan. The variation is read out of " +
			"its parent plan (GET /subscription-plans/{plan_id}) and located by id.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Description: "Variation UUID to look up.", Required: true},
			"plan_id":        schema.StringAttribute{Description: "UUID of the parent plan.", Required: true},
			"trial_duration": schema.StringAttribute{Description: "ISO-8601 trial duration for this variation.", Computed: true},
			"phases": schema.ListNestedAttribute{
				Description: "Ordered billing phases of this variation.",
				Computed:    true,
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

func (d *planVariationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req, resp)
}

func (d *planVariationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data planVariationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan, err := d.client.GetPlan(ctx, data.PlanID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.Diagnostics.AddError(
				"Plan not found",
				fmt.Sprintf("Plan %q does not exist.", data.PlanID.ValueString()),
			)
			return
		}
		resp.Diagnostics.AddError("Reading plan failed", err.Error())
		return
	}

	var variation *client.Variation
	for i := range plan.Variations {
		if plan.Variations[i].ID == data.ID.ValueString() {
			variation = &plan.Variations[i]
			break
		}
	}
	if variation == nil {
		resp.Diagnostics.AddError(
			"Variation not found",
			fmt.Sprintf("Plan %q has no variation with id %q.", plan.ID, data.ID.ValueString()),
		)
		return
	}

	data.TrialDuration = planVariationPtrToString(variation.TrialDuration)
	phases, diags := planVariationPhasesToList(variation.Phases)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Phases = phases

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ---- conversion helpers (package-local; prefixed to avoid colliding with the
// plan data source's own mappers). ----

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

func planVariationPtrToString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func planVariationPtrToInt64(i *int64) types.Int64 {
	if i == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*i)
}

func planVariationPhasesToList(phases []client.Phase) (types.List, diag.Diagnostics) {
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
