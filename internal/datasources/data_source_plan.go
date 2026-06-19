package datasources

import (
	"context"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*planDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*planDataSource)(nil)
)

// NewPlanDataSource is the revolut_plan data source constructor.
func NewPlanDataSource() datasource.DataSource {
	return &planDataSource{}
}

type planDataSource struct {
	client *client.Client
}

type planDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	TrialDuration types.String `tfsdk:"trial_duration"`
	State         types.String `tfsdk:"state"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
	Variations    types.List   `tfsdk:"variations"`
}

// ---- Object attribute types for the nested tree -------------------------------

func subscriptionItemAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":     types.StringType,
		"amount":   types.Int64Type,
		"quantity": types.Int64Type,
	}
}

func phaseAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":             types.StringType,
		"ordinal":        types.Int64Type,
		"cycle_duration": types.StringType,
		"cycle_count":    types.Int64Type,
		"amount":         types.Int64Type,
		"currency":       types.StringType,
		"subscription_items": types.ListType{
			ElemType: types.ObjectType{AttrTypes: subscriptionItemAttrTypes()},
		},
	}
}

func variationAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":             types.StringType,
		"trial_duration": types.StringType,
		"phases": types.ListType{
			ElemType: types.ObjectType{AttrTypes: phaseAttrTypes()},
		},
	}
}

func (d *planDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plan"
}

func (d *planDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Revolut subscription plan by id, including its full variations -> phases -> subscription_items pricing tree.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Plan UUID to look up.",
				Required:    true,
			},
			"name":           schema.StringAttribute{Description: "Plan name.", Computed: true},
			"trial_duration": schema.StringAttribute{Description: "ISO-8601 trial duration.", Computed: true},
			"state":          schema.StringAttribute{Description: "Plan lifecycle state.", Computed: true},
			"created_at":     schema.StringAttribute{Description: "Creation timestamp.", Computed: true},
			"updated_at":     schema.StringAttribute{Description: "Last update timestamp.", Computed: true},
			"variations": schema.ListNestedAttribute{
				Description: "Pricing options for the plan.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{Description: "Variation UUID.", Computed: true},
						"trial_duration": schema.StringAttribute{Description: "ISO-8601 trial duration for this variation.", Computed: true},
						"phases": schema.ListNestedAttribute{
							Description: "Billing stages of this variation.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"id":             schema.StringAttribute{Description: "Phase UUID.", Computed: true},
									"ordinal":        schema.Int64Attribute{Description: "1-based sequence position.", Computed: true},
									"cycle_duration": schema.StringAttribute{Description: "ISO-8601 billing cycle duration.", Computed: true},
									"cycle_count":    schema.Int64Attribute{Description: "Number of cycles (null = indefinite).", Computed: true},
									"amount":         schema.Int64Attribute{Description: "Charge amount in minor units.", Computed: true},
									"currency":       schema.StringAttribute{Description: "ISO 4217 currency code.", Computed: true},
									"subscription_items": schema.ListNestedAttribute{
										Description: "Line items billed within this phase.",
										Computed:    true,
										NestedObject: schema.NestedAttributeObject{
											Attributes: map[string]schema.Attribute{
												"type":     schema.StringAttribute{Description: "Item type: flat or usage.", Computed: true},
												"amount":   schema.Int64Attribute{Description: "Item amount in minor units.", Computed: true},
												"quantity": schema.Int64Attribute{Description: "Item quantity.", Computed: true},
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

func (d *planDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req, resp)
}

func (d *planDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg planDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := d.client.GetPlan(ctx, cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading plan", err.Error())
		return
	}

	cfg.ID = types.StringValue(got.ID)
	cfg.Name = types.StringValue(got.Name)
	cfg.TrialDuration = ptrToString(got.TrialDuration)
	cfg.State = stringOrNull(got.State)
	cfg.CreatedAt = stringOrNull(got.CreatedAt)
	cfg.UpdatedAt = stringOrNull(got.UpdatedAt)

	vlist, d2 := variationsToList(ctx, got.Variations)
	resp.Diagnostics.Append(d2...)
	if resp.Diagnostics.HasError() {
		return
	}
	cfg.Variations = vlist

	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// ---- API -> framework mapping -------------------------------------------------

func variationsToList(ctx context.Context, vars []client.Variation) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	objs := make([]attr.Value, 0, len(vars))
	for _, v := range vars {
		plist, d := phasesToList(ctx, v.Phases)
		diags.Append(d...)
		obj, d := types.ObjectValue(variationAttrTypes(), map[string]attr.Value{
			"id":             stringOrNull(v.ID),
			"trial_duration": ptrToString(v.TrialDuration),
			"phases":         plist,
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: variationAttrTypes()}, objs)
	diags.Append(d...)
	return list, diags
}

func phasesToList(_ context.Context, phases []client.Phase) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	objs := make([]attr.Value, 0, len(phases))
	for _, p := range phases {
		ilist, d := itemsToList(p.SubscriptionItems)
		diags.Append(d...)
		obj, d := types.ObjectValue(phaseAttrTypes(), map[string]attr.Value{
			"id":                 stringOrNull(p.ID),
			"ordinal":            types.Int64Value(p.Ordinal),
			"cycle_duration":     types.StringValue(p.CycleDuration),
			"cycle_count":        ptrToInt64(p.CycleCount),
			"amount":             ptrToInt64(p.Amount),
			"currency":           ptrToString(p.Currency),
			"subscription_items": ilist,
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: phaseAttrTypes()}, objs)
	diags.Append(d...)
	return list, diags
}

func itemsToList(items []client.SubscriptionItem) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	itemType := types.ObjectType{AttrTypes: subscriptionItemAttrTypes()}
	objs := make([]attr.Value, 0, len(items))
	for _, it := range items {
		obj, d := types.ObjectValue(subscriptionItemAttrTypes(), map[string]attr.Value{
			"type":     types.StringValue(it.Type),
			"amount":   ptrToInt64(it.Amount),
			"quantity": ptrToInt64(it.Quantity),
		})
		diags.Append(d...)
		objs = append(objs, obj)
	}
	list, d := types.ListValue(itemType, objs)
	diags.Append(d...)
	return list, diags
}

func ptrToString(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func ptrToInt64(p *int64) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
