// Command http-client demonstrates MicroJet's outbound JSON client (httpx.Client):
// typed GET/POST, default and per-request headers, non-2xx responses surfaced as
// structured *errorx.Error, automatic retries on transient failures, and a
// circuit breaker that fails fast when an upstream is down.
//
// It starts a local test server in-process so it runs offline:
//
//	go run .
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/httpx"
)

type echo struct {
	Method string `json:"method"`
	Auth   string `json:"auth"`
	Name   string `json:"name"`
}

func main() {
	srv := newTestServer()
	defer srv.Close()
	ctx := context.Background()

	// 1. A client with a base URL and a default header applied to every request.
	client := httpx.NewClient(srv.URL, httpx.WithDefaultHeader("Authorization", "Bearer default-token"))

	// GET decoding into a typed struct.
	var got echo
	if err := client.GetJSON(ctx, "/echo", &got); err != nil {
		panic(err)
	}
	fmt.Println("== GET with default header ==")
	fmt.Printf("  %+v\n", got)

	// POST a JSON body; override the auth header for just this call.
	var posted echo
	err := client.PostJSON(ctx, "/echo", map[string]string{"name": "Ada"}, &posted,
		httpx.WithHeader("Authorization", "Bearer per-request-token"))
	if err != nil {
		panic(err)
	}
	fmt.Println("\n== POST with per-request header ==")
	fmt.Printf("  %+v\n", posted)

	// 2. Non-2xx becomes a typed *errorx.Error you can inspect.
	err = client.GetJSON(ctx, "/not-found", nil)
	fmt.Println("\n== non-2xx -> structured error ==")
	fmt.Printf("  isInternal=%v err=%v\n", errorx.IsInternalError(err), err)

	// 3. Retries. /flaky fails twice with 503, then succeeds. With WithRetry the
	// client transparently re-attempts and the caller sees only success.
	retrying := httpx.NewClient(srv.URL, httpx.WithRetry(3, 10*time.Millisecond))
	if err := retrying.GetJSON(ctx, "/flaky", nil); err != nil {
		fmt.Printf("\n== retry == unexpected failure: %v\n", err)
	} else {
		fmt.Printf("\n== retry ==\n  /flaky succeeded after %d transient failures\n", flakyFailures)
	}

	// 4. Circuit breaker. /down always fails; after the threshold of consecutive
	// failures the breaker opens and further calls fail fast without hitting the
	// network (note the "circuit breaker open" message).
	breaking := httpx.NewClient(srv.URL, httpx.WithCircuitBreaker(2, time.Second))
	fmt.Println("\n== circuit breaker ==")
	for i := 1; i <= 4; i++ {
		err := breaking.GetJSON(ctx, "/down", nil)
		fmt.Printf("  call %d: %v\n", i, err)
	}
}

const flakyThreshold = 2

var (
	flakyCount    atomic.Int32
	flakyFailures = flakyThreshold
)

func newTestServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, echo{
			Method: r.Method,
			Auth:   r.Header.Get("Authorization"),
			Name:   body.Name,
		})
	})

	mux.HandleFunc("/not-found", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "missing"})
	})

	// Fails the first flakyThreshold times with 503, then returns 200.
	mux.HandleFunc("/flaky", func(w http.ResponseWriter, _ *http.Request) {
		if flakyCount.Add(1) <= int32(flakyThreshold) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/down", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	return httptest.NewServer(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(errors.New("encoding test response: " + err.Error()))
	}
}
