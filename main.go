// terraform-provider-revolut is a Terraform provider for the Revolut Merchant
// API (subscription plans and webhooks).
//
// Not affiliated with, endorsed by, or sponsored by Revolut.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

// version is set by goreleaser via -ldflags at release time.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// PUBLIC Terraform Registry address (unlike the private slyreply provider).
		Address: "registry.terraform.io/Sherman-Studio/revolut",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
