// Package datasources implements the Revolut provider data sources.
package datasources

import (
	"fmt"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

// clientFromProviderData type-asserts the provider-stashed *client.Client. It is
// shared by every data source's Configure. A nil ProviderData (early CLI walk)
// is not an error.
func clientFromProviderData(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got %T. Please report this to the provider developers.", req.ProviderData),
		)
		return nil
	}
	return c
}
