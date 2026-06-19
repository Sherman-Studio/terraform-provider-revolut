package datasources_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// ---- mock Merchant API ----------------------------------------------------

// pvDSMock serves GET /subscription-plans/{id} for the data source tests. The
// data source reads a variation out of its parent plan, so only that endpoint
// is exercised.
type pvDSMock struct {
	mu       sync.Mutex
	plan     map[string]any
	notFound bool
	apiError bool
}

func newPVDSMock() *pvDSMock {
	return &pvDSMock{
		plan: map[string]any{
			"id":    "plan-1",
			"name":  "Pro",
			"state": "active",
			"variations": []any{
				map[string]any{
					"id":             "var-eur",
					"trial_duration": "P7D",
					"phases": []any{
						map[string]any{
							"id":             "phase-eur-1",
							"ordinal":        float64(0),
							"cycle_duration": "P1M",
							"amount":         float64(799),
							"currency":       "EUR",
							"subscription_items": []any{
								map[string]any{"type": "flat", "amount": float64(799)},
							},
						},
					},
				},
			},
		},
	}
}

func (m *pvDSMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, "/api/subscription-plans/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if m.apiError {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"code":"upstream","message":"bad gateway"}`))
			return
		}
		if m.notFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.plan)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ---- harness --------------------------------------------------------------

func pvProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"revolut": func() (tfprotov6.ProviderServer, error) {
			return providerserver.NewProtocol6WithError(provider.New("test")())()
		},
	}
}

func pvPreCheck(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("TF_ACC", "1")
	t.Setenv("REVOLUT_API_KEY", "sk_test_dummy")
	t.Setenv("REVOLUT_ENDPOINT", endpoint)
}

const pvProviderBlock = `
provider "revolut" {}
`

// ---- tests ----------------------------------------------------------------

// TestAccPlanVariationDataSource_read reads a variation out of its plan by id.
func TestAccPlanVariationDataSource_read(t *testing.T) {
	mock := newPVDSMock()
	srv := mock.server(t)

	config := pvProviderBlock + `
data "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "var-eur"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { pvPreCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: pvProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "id", "var-eur"),
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "plan_id", "plan-1"),
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "trial_duration", "P7D"),
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "phases.#", "1"),
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "phases.0.id", "phase-eur-1"),
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "phases.0.currency", "EUR"),
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "phases.0.amount", "799"),
					resource.TestCheckResourceAttr("data.revolut_plan_variation.test", "phases.0.subscription_items.0.type", "flat"),
				),
			},
		},
	})
}

// TestAccPlanVariationDataSource_notInPlan errors when the id is absent.
func TestAccPlanVariationDataSource_notInPlan(t *testing.T) {
	mock := newPVDSMock()
	srv := mock.server(t)

	config := pvProviderBlock + `
data "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "does-not-exist"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { pvPreCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: pvProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile("Variation not found"),
			},
		},
	})
}

// TestAccPlanVariationDataSource_apiError surfaces a non-2xx plan read.
func TestAccPlanVariationDataSource_apiError(t *testing.T) {
	mock := newPVDSMock()
	mock.apiError = true
	srv := mock.server(t)

	config := pvProviderBlock + `
data "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "var-eur"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { pvPreCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: pvProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile("Reading plan failed|revolut API error 502"),
			},
		},
	})
}

// TestAccPlanVariationDataSource_planNotFound errors when the plan 404s.
func TestAccPlanVariationDataSource_planNotFound(t *testing.T) {
	mock := newPVDSMock()
	mock.notFound = true
	srv := mock.server(t)

	config := pvProviderBlock + `
data "revolut_plan_variation" "test" {
  plan_id = "missing"
  id      = "var-eur"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { pvPreCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: pvProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile("Plan not found"),
			},
		},
	})
}
