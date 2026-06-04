package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hatami57/microjet/core"
)

func TestClientPostJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("X-Api-Key") != "secret" {
			t.Errorf("missing default header, got %q", r.Header.Get("X-Api-Key"))
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]string{"echo": in["name"]})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithDefaultHeader("X-Api-Key", "secret"))
	var out struct {
		Echo string `json:"echo"`
	}
	if err := c.PostJSON(context.Background(), "/things", map[string]string{"name": "bob"}, &out); err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if out.Echo != "bob" {
		t.Errorf("echo = %q, want bob", out.Echo)
	}
}

func TestClientPerRequestHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer t1" {
			t.Errorf("Authorization = %q, want Bearer t1", got)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.GetJSON(context.Background(), "/", nil, WithHeader("Authorization", "Bearer t1")); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
}

func TestClientNon2xxReturnsStructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.GetJSON(context.Background(), "/", nil)
	if err == nil {
		t.Fatal("expected error for 502 response")
	}
	if !core.IsInternalError(err) {
		t.Errorf("error type = %T %v, want Internal", err, err)
	}
	ce := core.GetError(err)
	if ce == nil || ce.Params["status"] != 502 {
		t.Errorf("error params = %+v, want status 502", ce)
	}
}

func TestClientAbsolutePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// No base URL: an absolute path must still work.
	c := NewClient("")
	if err := c.GetJSON(context.Background(), srv.URL+"/abs", nil); err != nil {
		t.Fatalf("GetJSON absolute: %v", err)
	}
}
