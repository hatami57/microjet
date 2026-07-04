package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func bodyLimitRouter(maxBytes int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(maxBytes))
	r.POST("/", func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.String(http.StatusOK, "read %d bytes", len(b))
	})
	return r
}

func TestBodyLimitRejectsDeclaredOversizeBody(t *testing.T) {
	r := bodyLimitRouter(8)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("way too many bytes"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "request_entity_too_large") {
		t.Errorf("body = %s, want 413 payload", rec.Body.String())
	}
}

func TestBodyLimitAllowsWithinLimit(t *testing.T) {
	r := bodyLimitRouter(64)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestBodyLimitCapsChunkedRead verifies MaxBytesReader backstops a body that
// hides its size (no declared Content-Length), so a handler cannot read past the
// limit even when the up-front Content-Length check does not fire.
func TestBodyLimitCapsChunkedRead(t *testing.T) {
	r := bodyLimitRouter(8)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("way too many bytes"))
	req.ContentLength = -1 // undeclared length, forces the reader-level cap
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200, want the oversize read to be rejected")
	}
}

func TestBodyLimitDisabledWhenNonPositive(t *testing.T) {
	r := bodyLimitRouter(0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("anything at all goes"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (limit disabled)", rec.Code)
	}
}
