package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	router := gin.New()
	router.Use(RequestID())

	var seenInHandler string
	router.GET("/", func(c *gin.Context) {
		seenInHandler = RequestIDFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header().Get(DefaultRequestIDHeader)
	if got == "" {
		t.Fatal("expected a generated request id in the response header")
	}
	if seenInHandler != got {
		t.Errorf("handler saw %q, response header has %q; want equal", seenInHandler, got)
	}
}

func TestRequestIDPreservedWhenProvided(t *testing.T) {
	router := gin.New()
	router.Use(RequestID())
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(DefaultRequestIDHeader, "abc-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get(DefaultRequestIDHeader); got != "abc-123" {
		t.Errorf("request id = %q, want preserved %q", got, "abc-123")
	}
}

func TestRequestIDCustomHeader(t *testing.T) {
	router := gin.New()
	router.Use(RequestID(WithRequestIDHeader("X-Correlation-ID")))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-ID", "corr-9")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Correlation-ID"); got != "corr-9" {
		t.Errorf("custom-header request id = %q, want %q", got, "corr-9")
	}
}
