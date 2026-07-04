package httpx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hatami57/microjet/core"
)

func newTestServer() *Server {
	return NewServer(ServerConfig{Host: "localhost", Port: 0}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestReadyzNoChecksIsOK(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	s.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("body = %s, want ok", rec.Body.String())
	}
}

func TestReadyzReportsFailingCheck(t *testing.T) {
	s := newTestServer()
	s.AddReadinessCheck("good", func(context.Context) error { return nil })
	s.AddReadinessCheck("bad", func(context.Context) error { return errors.New("boom") })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	s.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"unavailable"`) {
		t.Errorf("body = %s, want unavailable", body)
	}
	if !strings.Contains(body, "boom") {
		t.Errorf("body = %s, want failing check detail", body)
	}
}

func TestPprofServedInDebugMode(t *testing.T) {
	s := NewServer(ServerConfig{Host: "localhost", Port: 0, Debug: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/goroutine", "/debug/pprof/cmdline"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		s.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestPprofAbsentInReleaseMode(t *testing.T) {
	s := newTestServer()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	s.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /debug/pprof/ status = %d, want 404", rec.Code)
	}
}

func TestReadyzShuttingDownAfterSetReady(t *testing.T) {
	s := newTestServer()
	s.AddReadinessCheck("good", func(context.Context) error { return nil })
	s.SetReady(false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	s.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"shutting-down"`) {
		t.Errorf("body = %s, want shutting-down", body)
	}

	// Flipping back to ready restores the normal check path.
	s.SetReady(true)
	rec = httptest.NewRecorder()
	s.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status after SetReady(true) = %d, want 200", rec.Code)
	}
}

func TestHealthStaysOKAfterSetReady(t *testing.T) {
	s := newTestServer()
	s.SetReady(false)

	rec := httptest.NewRecorder()
	s.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("liveness status = %d, want 200 while shutting down", rec.Code)
	}
}

// TestServerImplementsReadinessToggler guards the compile-time contract the host
// relies on to flip readiness at shutdown.
func TestServerImplementsReadinessToggler(t *testing.T) {
	var _ core.ReadinessToggler = newTestServer()
}

func TestInitAppliesConfiguredTimeouts(t *testing.T) {
	s := NewServer(ServerConfig{
		Host:              "localhost",
		Port:              0,
		ReadTimeout:       11 * time.Second,
		ReadHeaderTimeout: 6 * time.Second,
		WriteTimeout:      12 * time.Second,
		IdleTimeout:       70 * time.Second,
		MaxHeaderBytes:    2 << 20,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := s.httpServer.ReadTimeout; got != 11*time.Second {
		t.Errorf("ReadTimeout = %v, want 11s", got)
	}
	if got := s.httpServer.ReadHeaderTimeout; got != 6*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 6s", got)
	}
	if got := s.httpServer.WriteTimeout; got != 12*time.Second {
		t.Errorf("WriteTimeout = %v, want 12s", got)
	}
	if got := s.httpServer.IdleTimeout; got != 70*time.Second {
		t.Errorf("IdleTimeout = %v, want 70s", got)
	}
	if got := s.httpServer.MaxHeaderBytes; got != 2<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d", got, 2<<20)
	}
}
