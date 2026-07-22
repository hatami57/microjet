package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core/errorx"
)

func init() { gin.SetMode(gin.TestMode) }

func runWith(debug bool, handler gin.HandlerFunc) (*httptest.ResponseRecorder, errorx.ErrorResponse) {
	r := gin.New()
	r.Use(Error(debug))
	r.GET("/", handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	var resp errorx.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func TestTypedErrorMapsToStatus(t *testing.T) {
	w, resp := runWith(false, func(c *gin.Context) {
		c.Error(errorx.ErrNotFound.WithSubject("User"))
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if resp.Error != "not_found" || resp.Subject != "User" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestTypedErrorIncludesParams(t *testing.T) {
	_, resp := runWith(false, func(c *gin.Context) {
		c.Error(errorx.ErrBadRequest.WithSubject("email").WithParams("field", "email"))
	})
	if resp.Params == nil || resp.Params["field"] != "email" {
		t.Errorf("params not propagated: %+v", resp.Params)
	}
}

func TestInnerCauseHiddenWithoutDebug(t *testing.T) {
	secret := "connection string user:pass@db"
	_, resp := runWith(false, func(c *gin.Context) {
		c.Error(errorx.ErrInternal.WithInner(errors.New(secret)))
	})
	if resp.InnerError != nil {
		t.Errorf("inner cause leaked in production: %q", *resp.InnerError)
	}
}

func TestInnerCauseShownWithDebug(t *testing.T) {
	cause := "boom"
	_, resp := runWith(true, func(c *gin.Context) {
		c.Error(errorx.ErrInternal.WithInner(errors.New(cause)))
	})
	if resp.InnerError == nil || *resp.InnerError != cause {
		t.Errorf("inner cause not exposed in debug: %v", resp.InnerError)
	}
}

func TestMultipleErrorsAllLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := gin.New()
	// Inject a capturing logger the way the Logger middleware would.
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(ContextWithLogger(c.Request.Context(), logger))
	})
	r.Use(Error(false))
	r.GET("/", func(c *gin.Context) {
		c.Error(errorx.ErrBadRequest.WithSubject("email"))
		c.Error(errorx.ErrInternal.WithInner(errors.New("db down")))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	// The response renders only the last error (the internal 500).
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}

	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("bad log line %q: %v", raw, err)
		}
		lines = append(lines, m)
	}

	// Both attached errors are logged, each at its own level.
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2: %v", len(lines), lines)
	}
	// First: client-caused BadRequest at WARN.
	if lines[0]["level"] != "WARN" || lines[0]["subject"] != "email" {
		t.Errorf("first line = %v, want WARN for subject email", lines[0])
	}
	// Second: internal fault at ERROR, carrying the inner cause.
	if lines[1]["level"] != "ERROR" || lines[1]["cause"] != "db down" {
		t.Errorf("second line = %v, want ERROR with cause \"db down\"", lines[1])
	}
}

func TestUntypedErrorIsGenericInProduction(t *testing.T) {
	_, resp := runWith(false, func(c *gin.Context) {
		c.Error(errors.New("raw internal detail"))
	})
	if resp.InnerError != nil {
		t.Errorf("untyped error leaked detail: %q", *resp.InnerError)
	}
	if resp.Message != "An internal server error occurred" {
		t.Errorf("message = %q", resp.Message)
	}
}
