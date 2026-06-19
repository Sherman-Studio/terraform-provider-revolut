package datasources_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var protoV6Factories = map[string]func() (tfprotov6.ProviderServer, error){
	"revolut": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// startWebhookGetMock serves GET /webhooks/{id}. A {id} of "missing" 404s.
func startWebhookGetMock(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/webhooks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Path[len("/webhooks/"):]
		if id == "missing" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "not_found", "message": "no such webhook"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             id,
			"url":            "https://example.com/hook",
			"events":         []string{"ORDER_COMPLETED", "ORDER_AUTHORISED"},
			"signing_secret": "wsk_" + id,
		})
	})

	srv := httptest.NewServer(mux)
	t.Setenv("REVOLUT_ENDPOINT", srv.URL)
	t.Setenv("REVOLUT_API_KEY", "test-secret")
	t.Cleanup(srv.Close)
}

func TestAccWebhookDataSource_byID(t *testing.T) {
	startWebhookGetMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "revolut_webhook" "by_id" {
  id = "wh_42"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.revolut_webhook.by_id", "id", "wh_42"),
					resource.TestCheckResourceAttr("data.revolut_webhook.by_id", "url", "https://example.com/hook"),
					resource.TestCheckResourceAttr("data.revolut_webhook.by_id", "events.#", "2"),
					resource.TestCheckTypeSetElemAttr("data.revolut_webhook.by_id", "events.*", "ORDER_COMPLETED"),
					resource.TestCheckResourceAttr("data.revolut_webhook.by_id", "signing_secret", "wsk_wh_42"),
				),
			},
		},
	})
}

func TestAccWebhookDataSource_notFound(t *testing.T) {
	startWebhookGetMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "revolut_webhook" "missing" {
  id = "missing"
}
`,
				ExpectError: regexp.MustCompile(`Revolut webhook not found`),
			},
		},
	})
}

func TestMain(m *testing.M) {
	_ = os.Setenv("TF_ACC", "1")
	os.Exit(m.Run())
}
