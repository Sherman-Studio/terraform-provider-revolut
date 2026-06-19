// Package resources implements the Revolut provider managed resources.
package resources

import (
	"fmt"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// clientFromProviderData type-asserts the provider-stashed *client.Client. It is
// shared by every resource's Configure. A nil ProviderData (early CLI walk) is
// not an error.
func clientFromProviderData(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got %T. Please report this to the provider developers.", req.ProviderData),
		)
		return nil
	}
	return c
}
