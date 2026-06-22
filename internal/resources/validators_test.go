package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestHTTPSURLValidator(t *testing.T) {
	cases := []struct {
		name    string
		value   types.String
		wantErr bool
	}{
		{"https ok", types.StringValue("https://example.com/hook"), false},
		{"http rejected", types.StringValue("http://example.com/hook"), true},
		{"scheme-less rejected", types.StringValue("example.com/hook"), true},
		{"uppercase scheme ok", types.StringValue("HTTPS://example.com"), false},
		{"null skipped", types.StringNull(), false},
		{"unknown skipped", types.StringUnknown(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.StringRequest{Path: path.Root("url"), ConfigValue: tc.value}
			resp := &validator.StringResponse{}
			httpsURL().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Fatalf("HasError() = %v, want %v (diags: %v)", got, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestKnownEventsValidator(t *testing.T) {
	mk := func(vals ...string) types.Set {
		elems := make([]attr.Value, len(vals))
		for i, v := range vals {
			elems[i] = types.StringValue(v)
		}
		s, _ := types.SetValue(types.StringType, elems)
		return s
	}

	cases := []struct {
		name     string
		value    types.Set
		wantWarn bool
	}{
		{"all known", mk("ORDER_COMPLETED", "SUBSCRIPTION_INITIATED"), false},
		{"typo warns", mk("ORDER_COMPLETE"), true},
		{"one bad among good warns", mk("ORDER_COMPLETED", "SUBSCRIPTON_INITIATED"), true},
		{"null skipped", types.SetNull(types.StringType), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.SetRequest{Path: path.Root("events"), ConfigValue: tc.value}
			resp := &validator.SetResponse{}
			knownEvents().ValidateSet(context.Background(), req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected error diags: %v", resp.Diagnostics)
			}
			gotWarn := resp.Diagnostics.WarningsCount() > 0
			if gotWarn != tc.wantWarn {
				t.Fatalf("warning = %v, want %v (diags: %v)", gotWarn, tc.wantWarn, resp.Diagnostics)
			}
		})
	}
}
