//go:build tools

// Package tools pins build-time-only tool dependencies so `go mod tidy` keeps
// them as direct deps. It is never compiled into the provider binary.
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
