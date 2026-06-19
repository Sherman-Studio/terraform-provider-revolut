package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoSetsAuthAndVersionHeaders(t *testing.T) {
	var gotAuth, gotVersion, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("Revolut-Api-Version")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk_test_123", "2024-09-01")
	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.do(context.Background(), "GET", "/ping", nil, &out); err != nil {
		t.Fatalf("do: %v", err)
	}
	if gotAuth != "Bearer sk_test_123" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotVersion != "2024-09-01" {
		t.Fatalf("Revolut-Api-Version = %q", gotVersion)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if !out.OK {
		t.Fatal("response not decoded")
	}
}

func TestDoNotFoundReturnsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "")
	err := c.do(context.Background(), "GET", "/missing", nil, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDoAPIErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"code":"limit_exceeded","message":"max 10 webhooks"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "")
	err := c.do(context.Background(), "POST", "/webhooks", map[string]string{"url": "x"}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != 422 || apiErr.Code != "limit_exceeded" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestNewDefaultsAPIVersion(t *testing.T) {
	c := New("https://x", "tok", "")
	if c.apiVersion != DefaultAPIVersion {
		t.Fatalf("apiVersion = %q, want %q", c.apiVersion, DefaultAPIVersion)
	}
}
