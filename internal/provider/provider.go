// Package provider implements the Terraform provider for the Revolut Merchant API.
package provider

import (
	"context"
	"os"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"
	"github.com/Sherman-Studio/terraform-provider-revolut/internal/datasources"
	"github.com/Sherman-Studio/terraform-provider-revolut/internal/resources"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure revolutProvider satisfies the provider.Provider interface.
var _ provider.Provider = (*revolutProvider)(nil)

// New returns a provider factory bound to the given version (set by main /
// goreleaser ldflags).
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &revolutProvider{version: version}
	}
}

type revolutProvider struct {
	version string
}

// providerModel maps the provider configuration block.
type providerModel struct {
	APISecretKey types.String `tfsdk:"api_secret_key"`
	APIVersion   types.String `tfsdk:"api_version"`
	Sandbox      types.Bool   `tfsdk:"sandbox"`
}

func (p *revolutProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "revolut"
	resp.Version = p.version
}

func (p *revolutProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Revolut Merchant API subscription plans and webhooks.",
		Attributes: map[string]schema.Attribute{
			"api_secret_key": schema.StringAttribute{
				Description: "Revolut Merchant API secret key. Falls back to the REVOLUT_API_KEY environment variable. Never stored in state.",
				Optional:    true,
				Sensitive:   true,
			},
			"api_version": schema.StringAttribute{
				Description: "Revolut-Api-Version header value (YYYY-MM-DD). Falls back to REVOLUT_API_VERSION, then defaults to " + client.DefaultAPIVersion + ".",
				Optional:    true,
			},
			"sandbox": schema.BoolAttribute{
				Description: "When true, target the sandbox host (sandbox-merchant.revolut.com). Falls back to REVOLUT_SANDBOX. Defaults to false (production).",
				Optional:    true,
			},
		},
	}
}

func (p *revolutProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Token: attribute -> REVOLUT_API_KEY env. Never defaulted via schema.
	token := firstNonEmpty(cfg.APISecretKey.ValueString(), os.Getenv("REVOLUT_API_KEY"))
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_secret_key"),
			"Missing Revolut API secret key",
			"Set the provider api_secret_key attribute or the REVOLUT_API_KEY environment variable.",
		)
		return
	}

	apiVersion := firstNonEmpty(cfg.APIVersion.ValueString(), os.Getenv("REVOLUT_API_VERSION"), client.DefaultAPIVersion)

	sandbox := cfg.Sandbox.ValueBool()
	if cfg.Sandbox.IsNull() {
		if env := os.Getenv("REVOLUT_SANDBOX"); env == "true" || env == "1" {
			sandbox = true
		}
	}

	endpoint := client.ProductionBaseURL
	if sandbox {
		endpoint = client.SandboxBaseURL
	}
	// Allow an explicit endpoint override for tests / private hosts.
	if env := os.Getenv("REVOLUT_ENDPOINT"); env != "" {
		endpoint = env
	}

	c := client.New(endpoint, token, apiVersion)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *revolutProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewPlanResource,
		resources.NewWebhookResource,
	}
}

func (p *revolutProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewPlanDataSource,
		datasources.NewWebhookDataSource,
	}
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
