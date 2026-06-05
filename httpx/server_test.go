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
