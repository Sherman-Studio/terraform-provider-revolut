package resources_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync"
	"testing"

	"github.com/Sherman-Studio/terraform-provider-revolut/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// protoV6Factories wires the real provider behind the test harness. The
// REVOLUT_ENDPOINT env var (read in provider.Configure) points the client at the
// httptest server, and REVOLUT_API_KEY satisfies the required-token check.
var protoV6Factories = map[string]func() (tfprotov6.ProviderServer, error){
	"revolut": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// webhookStore is a tiny in-memory mock of the Merchant webhook API. It backs
// the httptest server used by every webhook acceptance test.
type webhookStore struct {
	mu       sync.Mutex
	wh       map[string]*storedWebhook
	seq      int
	failNext bool // when true, the next mutating call returns 500
}

type storedWebhook struct {
	ID            string
	URL           string
	Events        []string
	SigningSecret string
}

func newWebhookStore() *webhookStore {
	return &webhookStore{wh: map[string]*storedWebhook{}}
}

func (s *webhookStore) handler() http.Handler {
	mux := http.NewServeMux()

	// Create + (list, unused by the provider).
	mux.HandleFunc("/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.failNext {
			s.failNext = false
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "boom")
			return
		}

		var body struct {
			URL    string   `json:"url"`
			Events []string `json:"events"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		s.seq++
		id := fmt.Sprintf("wh_%05d", s.seq)
		rec := &storedWebhook{
			ID:            id,
			URL:           body.URL,
			Events:        body.Events,
			SigningSecret: "wsk_" + id,
		}
		s.wh[id] = rec
		writeWebhook(w, http.StatusOK, rec)
	})

	// Read-one / patch / delete / rotate, all under /webhooks/{id}...
	mux.HandleFunc("/webhooks/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Path is /webhooks/{id} or /webhooks/{id}/rotate-signing-secret.
		rest := r.URL.Path[len("/webhooks/"):]
		rotate := false
		id := rest
		if suffix := "/rotate-signing-secret"; len(rest) > len(suffix) && rest[len(rest)-len(suffix):] == suffix {
			rotate = true
			id = rest[:len(rest)-len(suffix)]
		}

		rec, ok := s.wh[id]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found", "no such webhook")
			return
		}

		switch {
		case rotate && r.Method == http.MethodPost:
			rec.SigningSecret = "wsk_rotated_" + id
			writeWebhook(w, http.StatusOK, rec)
		case r.Method == http.MethodGet:
			writeWebhook(w, http.StatusOK, rec)
		case r.Method == http.MethodPatch:
			var body struct {
				URL    *string  `json:"url"`
				Events []string `json:"events"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.URL != nil {
				rec.URL = *body.URL
			}
			if body.Events != nil {
				rec.Events = body.Events
			}
			writeWebhook(w, http.StatusOK, rec)
		case r.Method == http.MethodDelete:
			delete(s.wh, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return mux
}

func writeWebhook(w http.ResponseWriter, status int, rec *storedWebhook) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":             rec.ID,
		"url":            rec.URL,
		"events":         rec.Events,
		"signing_secret": rec.SigningSecret,
	})
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": msg})
}

// startMockAPI spins up the httptest server, points the provider at it via env,
// and registers cleanup.
func startMockAPI(t *testing.T, store *webhookStore) {
	t.Helper()
	srv := httptest.NewServer(store.handler())
	t.Setenv("REVOLUT_ENDPOINT", srv.URL)
	t.Setenv("REVOLUT_API_KEY", "test-secret")
	t.Cleanup(srv.Close)
}

func TestAccWebhookResource_lifecycle(t *testing.T) {
	store := newWebhookStore()
	startMockAPI(t, store)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			// Create + Read.
			{
				Config: `
resource "revolut_webhook" "test" {
  url    = "https://example.com/hook"
  events = ["ORDER_COMPLETED"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("revolut_webhook.test", "url", "https://example.com/hook"),
					resource.TestCheckResourceAttr("revolut_webhook.test", "events.#", "1"),
					resource.TestCheckTypeSetElemAttr("revolut_webhook.test", "events.*", "ORDER_COMPLETED"),
					resource.TestCheckResourceAttrSet("revolut_webhook.test", "id"),
					resource.TestCheckResourceAttrSet("revolut_webhook.test", "signing_secret"),
					resource.TestMatchResourceAttr("revolut_webhook.test", "signing_secret", regexp.MustCompile(`^wsk_`)),
				),
			},
			// Update url + events in place (no replacement).
			{
				Config: `
resource "revolut_webhook" "test" {
  url    = "https://example.com/hook2"
  events = ["ORDER_COMPLETED", "ORDER_AUTHORISED"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("revolut_webhook.test", "url", "https://example.com/hook2"),
					resource.TestCheckResourceAttr("revolut_webhook.test", "events.#", "2"),
					resource.TestCheckTypeSetElemAttr("revolut_webhook.test", "events.*", "ORDER_AUTHORISED"),
				),
			},
			// Import: id passthrough, Read recovers everything incl. signing_secret.
			{
				ResourceName:      "revolut_webhook.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccWebhookResource_rotateSecret(t *testing.T) {
	store := newWebhookStore()
	startMockAPI(t, store)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "revolut_webhook" "test" {
  url            = "https://example.com/hook"
  events         = ["ORDER_COMPLETED"]
  rotate_trigger = "v1"
}
`,
				Check: resource.TestMatchResourceAttr("revolut_webhook.test", "signing_secret", regexp.MustCompile(`^wsk_wh_`)),
			},
			// Changing rotate_trigger calls rotate-signing-secret -> new secret.
			{
				Config: `
resource "revolut_webhook" "test" {
  url               = "https://example.com/hook"
  events            = ["ORDER_COMPLETED"]
  rotate_trigger    = "v2"
  expiration_period = "PT5H30M"
}
`,
				Check: resource.TestMatchResourceAttr("revolut_webhook.test", "signing_secret", regexp.MustCompile(`^wsk_rotated_`)),
			},
		},
	})
}

func TestAccWebhookResource_createError(t *testing.T) {
	store := newWebhookStore()
	store.failNext = true
	startMockAPI(t, store)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "revolut_webhook" "test" {
  url    = "https://example.com/hook"
  events = ["ORDER_COMPLETED"]
}
`,
				ExpectError: regexp.MustCompile(`Could not create Revolut webhook`),
			},
		},
	})
}

// TestMain enables the acceptance harness for the in-process mock server.
func TestMain(m *testing.M) {
	// resource.Test requires TF_ACC; our "API" is an in-process httptest server,
	// so no real Revolut credentials or network access are needed.
	_ = os.Setenv("TF_ACC", "1")
	os.Exit(m.Run())
}
