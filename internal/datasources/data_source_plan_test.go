package datasources_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// protoV6Factories is the shared provider factory map declared in
// data_source_webhook_test.go (same datasources_test package).

func configurePlanEnv(t *testing.T, url string) {
	t.Setenv("REVOLUT_ENDPOINT", url)
	t.Setenv("REVOLUT_API_KEY", "sk_test_fake")
	t.Setenv("REVOLUT_API_VERSION", "2024-09-01")
	t.Setenv("REVOLUT_SANDBOX", "false")
}

// planFixture is the canned plan the GET handler returns for the data-source
// read-by-id test.
var planFixture = map[string]any{
	"id":             "plan-00000001",
	"name":           "Pro",
	"trial_duration": "P14D",
	"state":          "active",
	"created_at":     "2026-06-19T00:00:00Z",
	"updated_at":     "2026-06-19T00:00:00Z",
	"variations": []any{
		map[string]any{
			"id":             "var-00000001",
			"trial_duration": "P7D",
			"phases": []any{
				map[string]any{
					"id":             "phase-00000001",
					"ordinal":        1,
					"cycle_duration": "P1M",
					"cycle_count":    12,
					"amount":         9900,
					"currency":       "GBP",
					"subscription_items": []any{
						map[string]any{"type": "flat", "amount": 9900, "quantity": 1},
					},
				},
			},
		},
	},
}

func planGetServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/subscription-plans/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/subscription-plans/"):]
		if id != planFixture["id"] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(planFixture)
	})
	return httptest.NewServer(mux)
}

const planDataSourceConfig = `
data "revolut_plan" "test" {
  id = "plan-00000001"
}
`

// TestAccPlanDataSource_readByID verifies the data source fetches a plan by id
// and projects the full variations -> phases -> subscription_items tree.
func TestAccPlanDataSource_readByID(t *testing.T) {
	srv := planGetServer(t)
	defer srv.Close()
	configurePlanEnv(t, srv.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: planDataSourceConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.revolut_plan.test", "id", "plan-00000001"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "name", "Pro"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "trial_duration", "P14D"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "state", "active"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.#", "1"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.id", "var-00000001"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.trial_duration", "P7D"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.phases.0.id", "phase-00000001"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.phases.0.cycle_duration", "P1M"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.phases.0.cycle_count", "12"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.phases.0.amount", "9900"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.phases.0.currency", "GBP"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.phases.0.subscription_items.0.type", "flat"),
					resource.TestCheckResourceAttr("data.revolut_plan.test", "variations.0.phases.0.subscription_items.0.amount", "9900"),
				),
			},
		},
	})
}

// TestAccPlanDataSource_readError verifies a non-2xx GET surfaces as an error.
func TestAccPlanDataSource_readError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"internal_error","message":"boom"}`))
	}))
	defer srv.Close()
	configurePlanEnv(t, srv.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config:      planDataSourceConfig,
				ExpectError: regexp.MustCompile(`internal_error|boom|Error reading plan`),
			},
		},
	})
}
