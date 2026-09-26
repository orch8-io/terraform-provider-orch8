package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewNormalizesEndpoint(t *testing.T) {
	cases := map[string]string{
		"":                                DefaultEndpoint + APIPrefix,
		"http://engine:8080":              "http://engine:8080/api/v1",
		"http://engine:8080/":             "http://engine:8080/api/v1",
		"https://x.example/api/v1":        "https://x.example/api/v1",
		"https://x.example/prefix/api/v1": "https://x.example/prefix/api/v1",
	}
	for in, want := range cases {
		c, err := New(in, "", "")
		if err != nil {
			t.Fatalf("New(%q): %v", in, err)
		}
		if c.BaseURL != want {
			t.Errorf("New(%q).BaseURL = %q, want %q", in, c.BaseURL, want)
		}
	}
	if _, err := New("not a url", "", ""); err == nil {
		t.Error("expected error for relative endpoint")
	}
}

func TestDoSendsAuthHeadersAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/things/a%2Fb" && r.URL.RawPath != "/api/v1/things/a%2Fb" {
			t.Errorf("path = %q raw %q", r.URL.Path, r.URL.RawPath)
		}
		if got := r.Header.Get("x-api-key"); got != "k" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("x-tenant-id"); got != "acme" {
			t.Errorf("x-tenant-id = %q", got)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "acme" {
			t.Errorf("query tenant_id = %q", got)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing content type")
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["hello"] != "world" {
			t.Errorf("body = %v", in)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "x1"})
	}))
	defer srv.Close()

	c, err := New(srv.URL, "k", "acme")
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ ID string }
	err = c.Do(context.Background(), "POST", "/things/"+PathEscape("a/b"), url.Values{"tenant_id": {"acme"}},
		map[string]string{"hello": "world"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "x1" {
		t.Fatalf("decoded id = %q", out.ID)
	}
}

func TestDoOmitsEmptyAuthHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["X-Api-Key"]; ok {
			t.Error("unexpected x-api-key header")
		}
		if _, ok := r.Header["X-Tenant-Id"]; ok {
			t.Error("unexpected x-tenant-id header")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "", "")
	if err := c.Do(context.Background(), "DELETE", "/x", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDoMapsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/missing":
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		default:
			http.Error(w, `{"error":"conflict"}`, http.StatusConflict)
		}
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "", "")
	err := c.Do(context.Background(), "GET", "/missing", nil, nil, nil)
	if !IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
	err = c.Do(context.Background(), "GET", "/busy", nil, nil, nil)
	if err == nil || IsNotFound(err) {
		t.Fatalf("expected non-404 error, got %v", err)
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("expected APIError 409, got %#v", err)
	}
}

func TestDoRetriesUnavailable(t *testing.T) {
	RetryBaseDelay = time.Millisecond
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			http.Error(w, `{"error":"unavailable: database is locked"}`, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "", "")
	if err := c.Do(context.Background(), "DELETE", "/x", nil, map[string]int{"a": 1}, nil); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 attempts, got %d", n)
	}
}
