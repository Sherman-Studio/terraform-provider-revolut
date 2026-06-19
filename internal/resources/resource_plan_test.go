package resources_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// protoV6Factories is the shared provider factory map declared in
// resource_webhook_test.go (same resources_test package). The provider reads
// REVOLUT_ENDPOINT / REVOLUT_API_KEY from the environment (set per test) to
// target the httptest server instead of the real Merchant API.

// fakePlanServer is an in-memory stand-in for the Revolut subscription-plans API.
// It assigns UUIDs to the plan + nested variations/phases/items on create and
// serves them back on GET. There is intentionally no update/delete endpoint —
// the plan resource is create-and-replace only.
type fakePlanServer struct {
	mu     sync.Mutex
	plans  map[string]map[string]any
	nextID int
}

func newFakePlanServer() *fakePlanServer {
	return &fakePlanServer{plans: map[string]map[string]any{}}
}

func (f *fakePlanServer) id(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s-%08d", prefix, f.nextID)
}

// stampIDs walks the inbound create body and assigns server UUIDs + computed
// fields, mimicking Revolut's behaviour.
func (f *fakePlanServer) stampIDs(body map[string]any) map[string]any {
	plan := map[string]any{}
	for k, v := range body {
		plan[k] = v
	}
	plan["id"] = f.id("plan")
	plan["state"] = "active"
	plan["created_at"] = "2026-06-19T00:00:00Z"
	plan["updated_at"] = "2026-06-19T00:00:00Z"

	vars, _ := plan["variations"].([]any)
	for vi := range vars {
		v, _ := vars[vi].(map[string]any)
		v["id"] = f.id("var")
		// The real API never echoes a per-variation trial_duration; mirror that so
		// the Computed attribute resolves to null.
		delete(v, "trial_duration")
		phases, _ := v["phases"].([]any)
		for pi := range phases {
			p, _ := phases[pi].(map[string]any)
			p["id"] = f.id("phase")
			items, _ := p["subscription_items"].([]any)
			for ii := range items {
				it, _ := items[ii].(map[string]any)
				it["id"] = f.id("item")
			}
		}
	}
	return plan
}

func (f *fakePlanServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/subscription-plans", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"code":"method_not_allowed","message":"only POST"}`, http.StatusMethodNotAllowed)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"code":"bad_request","message":"invalid json"}`, http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		plan := f.stampIDs(body)
		f.plans[plan["id"].(string)] = plan
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(plan)
	})

	mux.HandleFunc("/subscription-plans/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/subscription-plans/"):]
		f.mu.Lock()
		plan, ok := f.plans[id]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(plan)
	})

	return mux
}

// configurePlanEnv points the provider at the fake server.
func configurePlanEnv(t *testing.T, url string) {
	t.Setenv("REVOLUT_ENDPOINT", url)
	t.Setenv("REVOLUT_API_KEY", "sk_test_fake")
	t.Setenv("REVOLUT_API_VERSION", "2024-09-01")
	t.Setenv("REVOLUT_SANDBOX", "false")
}

const planConfigBasic = `
resource "revolut_plan" "test" {
  name           = "Pro"
  trial_duration = "P14D"

  variations = [
    {
      phases = [
        {
          ordinal        = 1
          cycle_duration = "P1M"
          cycle_count    = 12
          subscription_items = [
            {
              name     = "Base"
              unit     = "month"
              type     = "flat"
              amount   = 9900
              quantity = 1
              currency = "GBP"
            },
          ]
        },
      ]
    },
  ]
}
`

// planConfigReplaced changes an immutable field (name) which must force replace.
const planConfigReplaced = `
resource "revolut_plan" "test" {
  name = "Power"

  variations = [
    {
      phases = [
        {
          ordinal        = 1
          cycle_duration = "P1Y"
          amount         = 29900
          currency       = "GBP"
        },
      ]
    },
  ]
}
`

// TestAccPlanResource_lifecycle exercises create -> read -> update(replace) ->
// delete and import. The "update" changes an immutable field, so the framework
// plans a destroy+create (RequiresReplace) rather than an in-place update.
func TestAccPlanResource_lifecycle(t *testing.T) {
	fake := newFakePlanServer()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	configurePlanEnv(t, srv.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			// Create + Read.
			{
				Config: planConfigBasic,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("revolut_plan.test", "id", regexp.MustCompile(`^plan-`)),
					resource.TestCheckResourceAttr("revolut_plan.test", "name", "Pro"),
					resource.TestCheckResourceAttr("revolut_plan.test", "trial_duration", "P14D"),
					resource.TestCheckResourceAttr("revolut_plan.test", "state", "active"),
					resource.TestCheckResourceAttr("revolut_plan.test", "created_at", "2026-06-19T00:00:00Z"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.#", "1"),
					resource.TestMatchResourceAttr("revolut_plan.test", "variations.0.id", regexp.MustCompile(`^var-`)),
					resource.TestCheckNoResourceAttr("revolut_plan.test", "variations.0.trial_duration"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.#", "1"),
					resource.TestMatchResourceAttr("revolut_plan.test", "variations.0.phases.0.id", regexp.MustCompile(`^phase-`)),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.ordinal", "1"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.cycle_duration", "P1M"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.cycle_count", "12"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.#", "1"),
					resource.TestMatchResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.0.id", regexp.MustCompile(`^item-`)),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.0.name", "Base"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.0.unit", "month"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.0.type", "flat"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.0.amount", "9900"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.0.currency", "GBP"),
				),
			},
			// Import: passthrough id -> Read recovers all attributes.
			{
				ResourceName:      "revolut_plan.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// "Update" of an immutable field -> RequiresReplace (destroy+create).
			{
				Config: planConfigReplaced,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("revolut_plan.test", "name", "Power"),
					resource.TestCheckNoResourceAttr("revolut_plan.test", "trial_duration"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.cycle_duration", "P1Y"),
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.amount", "29900"),
					// cycle_count omitted -> null/indefinite.
					resource.TestCheckNoResourceAttr("revolut_plan.test", "variations.0.phases.0.cycle_count"),
					// no subscription_items configured -> empty.
					resource.TestCheckResourceAttr("revolut_plan.test", "variations.0.phases.0.subscription_items.#", "0"),
				),
			},
		},
	})
}

// TestAccPlanResource_createError verifies a non-2xx create surfaces as an error
// with Revolut's error envelope message.
func TestAccPlanResource_createError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"code":"validation_error","message":"variations must not be empty"}`))
	}))
	defer srv.Close()
	configurePlanEnv(t, srv.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config:      planConfigBasic,
				ExpectError: regexp.MustCompile(`validation_error|variations must not be empty`),
			},
		},
	})
}
