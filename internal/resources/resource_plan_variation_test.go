package resources_test

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

// planVariationMock is a tiny stateful stand-in for the Revolut Merchant API,
// scoped to GET /subscription-plans/{id}. Variations have no CRUD endpoints, so
// the resource only ever reads the parent plan; the mock lets the test mutate
// the served variation between steps to exercise drift/replace.
type planVariationMock struct {
	mu sync.Mutex

	// plan is the JSON body returned by GET /subscription-plans/{id}.
	plan     map[string]any
	notFound bool // when true, GET returns 404
	apiError bool // when true, GET returns a 500 error envelope
}

func newPlanVariationMock() *planVariationMock {
	m := &planVariationMock{}
	m.plan = map[string]any{
		"id":    "plan-1",
		"name":  "Pro",
		"state": "active",
		"variations": []any{
			map[string]any{
				"id":             "var-usd",
				"trial_duration": "P14D",
				"phases": []any{
					map[string]any{
						"id":             "phase-1",
						"ordinal":        float64(0),
						"cycle_duration": "P1M",
						"amount":         float64(900),
						"currency":       "USD",
						"subscription_items": []any{
							map[string]any{"type": "flat", "amount": float64(900)},
						},
					},
				},
			},
		},
	}
	return m
}

func (m *planVariationMock) server(t *testing.T) *httptest.Server {
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
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"internal_error","message":"boom"}`))
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

// setTrialDuration mutates the served variation's trial_duration to force a
// post-apply read diff (drift) on the next plan.
func (m *planVariationMock) setTrialDuration(d string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vars := m.plan["variations"].([]any)
	v := vars[0].(map[string]any)
	v["trial_duration"] = d
}

func (m *planVariationMock) setNotFound(v bool) { m.mu.Lock(); m.notFound = v; m.mu.Unlock() }
func (m *planVariationMock) setAPIError(v bool) { m.mu.Lock(); m.apiError = v; m.mu.Unlock() }

// ---- test harness ----------------------------------------------------------

func testProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"revolut": func() (tfprotov6.ProviderServer, error) {
			// The provider's Configure reads REVOLUT_ENDPOINT / REVOLUT_API_KEY.
			return providerserver.NewProtocol6WithError(provider.New("test")())()
		},
	}
}

// preCheck wires the provider to the mock and forces the acceptance harness on.
func preCheck(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("TF_ACC", "1")
	t.Setenv("REVOLUT_API_KEY", "sk_test_dummy")
	t.Setenv("REVOLUT_ENDPOINT", endpoint)
}

const providerBlock = `
provider "revolut" {}
`

// ---- tests -----------------------------------------------------------------

// TestAccPlanVariationResource_lifecycle covers create -> read -> in-place
// refresh (trial_duration drift) -> delete, plus import.
func TestAccPlanVariationResource_lifecycle(t *testing.T) {
	mock := newPlanVariationMock()
	srv := mock.server(t)

	config := providerBlock + `
resource "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "var-usd"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			// 1. Create + Read.
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "id", "var-usd"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "plan_id", "plan-1"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "trial_duration", "P14D"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "phases.#", "1"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "phases.0.id", "phase-1"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "phases.0.currency", "USD"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "phases.0.amount", "900"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "phases.0.subscription_items.#", "1"),
					resource.TestCheckResourceAttr("revolut_plan_variation.test", "phases.0.subscription_items.0.type", "flat"),
				),
			},
			// 2. Import via the "plan_id:variation_id" composite id.
			{
				ResourceName:      "revolut_plan_variation.test",
				ImportState:       true,
				ImportStateId:     "plan-1:var-usd",
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccPlanVariationResource_drift mutates the upstream variation between the
// apply and the next refresh; Read must surface the new trial_duration.
func TestAccPlanVariationResource_drift(t *testing.T) {
	mock := newPlanVariationMock()
	srv := mock.server(t)

	config := providerBlock + `
resource "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "var-usd"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("revolut_plan_variation.test", "trial_duration", "P14D"),
			},
			{
				// Change upstream, then refresh-only: Read must pull the new value
				// into state and the harness sees a non-empty (drifted) plan.
				PreConfig:          func() { mock.setTrialDuration("P30D") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              resource.TestCheckResourceAttr("revolut_plan_variation.test", "trial_duration", "P30D"),
			},
		},
	})
}

// TestAccPlanVariationResource_gone removes the variation upstream; Read should
// drop it from state, producing a plan that recreates it.
func TestAccPlanVariationResource_gone(t *testing.T) {
	mock := newPlanVariationMock()
	srv := mock.server(t)

	config := providerBlock + `
resource "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "var-usd"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				// Variation gone upstream: refresh-only Read removes it from state,
				// so the harness plans to recreate it (non-empty plan).
				PreConfig:          func() { mock.setNotFound(true) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccPlanVariationResource_apiError asserts a non-2xx (500) from the
// upstream plan read surfaces as a Terraform error during create.
func TestAccPlanVariationResource_apiError(t *testing.T) {
	mock := newPlanVariationMock()
	mock.setAPIError(true)
	srv := mock.server(t)

	config := providerBlock + `
resource "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "var-usd"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile("Reading parent plan failed|revolut API error 500"),
			},
		},
	})
}

// TestAccPlanVariationResource_badImportID rejects a non-composite import id.
func TestAccPlanVariationResource_badImportID(t *testing.T) {
	mock := newPlanVariationMock()
	srv := mock.server(t)

	config := providerBlock + `
resource "revolut_plan_variation" "test" {
  plan_id = "plan-1"
  id      = "var-usd"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t, srv.URL+"/api") },
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				ResourceName:  "revolut_plan_variation.test",
				ImportState:   true,
				ImportStateId: "not-a-composite",
				ExpectError:   regexp.MustCompile("Invalid import ID"),
			},
		},
	})
}
