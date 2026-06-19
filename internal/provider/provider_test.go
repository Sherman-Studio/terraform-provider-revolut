package provider

import (
	"context"
	"os"
	"testing"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderInterface(t *testing.T) {
	var _ provider.Provider = New("test")()
}

func TestProviderMetadata(t *testing.T) {
	p := New("test")()
	resp := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, resp)
	if resp.TypeName != "revolut" {
		t.Fatalf("TypeName = %q, want revolut", resp.TypeName)
	}
	if resp.Version != "test" {
		t.Fatalf("Version = %q, want test", resp.Version)
	}
}

func TestProviderResourceCount(t *testing.T) {
	p := New("test")()
	if got := len(p.Resources(context.Background())); got != 3 {
		t.Fatalf("Resources count = %d, want 3", got)
	}
	var _ []func() resource.Resource = p.Resources(context.Background())

	if got := len(p.DataSources(context.Background())); got != 3 {
		t.Fatalf("DataSources count = %d, want 3", got)
	}
	var _ []func() datasource.DataSource = p.DataSources(context.Background())
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x"); got != "x" {
		t.Fatalf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Fatalf("firstNonEmpty = %q, want a", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty = %q, want empty", got)
	}
}

// TestDefaultAPIVersion guards the documented default against accidental drift.
func TestDefaultAPIVersion(t *testing.T) {
	if client.DefaultAPIVersion != "2024-09-01" {
		t.Fatalf("DefaultAPIVersion = %q, want 2024-09-01", client.DefaultAPIVersion)
	}
}

// keep os import meaningful even if env-precedence tests are added later.
var _ = os.Getenv
