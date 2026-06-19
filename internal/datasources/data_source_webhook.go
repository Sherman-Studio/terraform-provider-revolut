package datasources

import (
	"context"
	"errors"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*webhookDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*webhookDataSource)(nil)
)

// NewWebhookDataSource is the revolut_webhook data source constructor.
func NewWebhookDataSource() datasource.DataSource {
	return &webhookDataSource{}
}

type webhookDataSource struct {
	client *client.Client
}

type webhookDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	URL           types.String `tfsdk:"url"`
	Events        types.Set    `tfsdk:"events"`
	SigningSecret types.String `tfsdk:"signing_secret"`
}

func (d *webhookDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (d *webhookDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Revolut Merchant webhook by id, including its sensitive signing_secret.",
		Attributes: map[string]schema.Attribute{
			"id":  schema.StringAttribute{Description: "Webhook UUID to look up.", Required: true},
			"url": schema.StringAttribute{Description: "HTTPS endpoint.", Computed: true},
			"events": schema.SetAttribute{
				Description: "Subscribed event types.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"signing_secret": schema.StringAttribute{
				Description: "Server-generated signing secret (wsk_...).",
				Computed:    true,
				Sensitive:   true,
			},
		},
	}
}

func (d *webhookDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req, resp)
}

func (d *webhookDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg webhookDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GET /webhooks/{id} returns signing_secret (the list endpoint omits it).
	wh, err := d.client.GetWebhook(ctx, cfg.ID.ValueString())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.Diagnostics.AddError(
				"Revolut webhook not found",
				"No webhook exists with id "+cfg.ID.ValueString()+".",
			)
			return
		}
		resp.Diagnostics.AddError("Could not read Revolut webhook", err.Error())
		return
	}

	state := webhookDataSourceModel{
		ID:  types.StringValue(wh.ID),
		URL: types.StringValue(wh.URL),
	}
	events, diags := types.SetValueFrom(ctx, types.StringType, wh.Events)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Events = events
	state.SigningSecret = types.StringValue(wh.SigningSecret)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
