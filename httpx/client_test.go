package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hatami57/microjet/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
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

func TestClientRetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithRetry(3, time.Millisecond))
	var out struct {
		OK string `json:"ok"`
	}
	if err := c.GetJSON(context.Background(), "/", &out); err != nil {
		t.Fatalf("GetJSON with retry: %v", err)
	}
	if out.OK != "yes" {
		t.Errorf("ok = %q, want yes", out.OK)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server saw %d calls, want 3 (2 retries)", got)
	}
}

func TestClientRetriesExhaustedReturnsError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithRetry(2, time.Millisecond))
	if err := c.GetJSON(context.Background(), "/", nil); err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server saw %d calls, want 3 (1 initial + 2 retries)", got)
	}
}

func TestClientDoesNotRetryNonIdempotentByDefault(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// POST is not in the default retryable method set.
	c := NewClient(srv.URL, WithRetry(3, time.Millisecond))
	if err := c.PostJSON(context.Background(), "/", map[string]string{"a": "b"}, nil); err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server saw %d calls, want 1 (POST not retried by default)", got)
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

func TestClientInjectsTraceContext(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTextMapPropagator(prev)

	var traceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	if err := NewClient(srv.URL).GetJSON(ctx, "/", nil); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	want := "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01"
	if traceparent != want {
		t.Errorf("traceparent = %q, want %q", traceparent, want)
	}
}
