package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTimeoutFastHandlerPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Timeout(50 * time.Millisecond))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "done") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "done" {
		t.Errorf("body = %q, want done", rec.Body.String())
	}
}

func TestTimeoutSlowHandlerReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Timeout(20 * time.Millisecond))
	// Handler ignores its context and writes late; the response must still be a
	// clean 503 and the late write must not corrupt it.
	r.GET("/", func(c *gin.Context) {
		time.Sleep(80 * time.Millisecond)
		c.String(http.StatusOK, "too late")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "timeout") {
		t.Errorf("body = %q, want timeout payload", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "too late") {
		t.Errorf("late handler output leaked into the 503 response: %q", rec.Body.String())
	}
	// Let the slow handler finish so its late writes race the assertions under -race.
	time.Sleep(120 * time.Millisecond)
}

func TestTimeoutCancelsRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Timeout(20 * time.Millisecond))
	var ctxErr error
	done := make(chan struct{})
	r.GET("/", func(c *gin.Context) {
		<-c.Request.Context().Done()
		ctxErr = c.Request.Context().Err()
		close(done)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	<-done

	if ctxErr == nil {
		t.Fatal("request context was not cancelled on timeout")
	}
}

func TestTimeoutRecoversHandlerPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(Timeout(50 * time.Millisecond))
	r.GET("/", func(c *gin.Context) { panic("boom") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 from recovery", rec.Code)
	}
}

func TestTimeoutDisabledWhenNonPositive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Timeout(0))
	r.GET("/", func(c *gin.Context) {
		time.Sleep(10 * time.Millisecond)
		c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (timeout disabled)", rec.Code)
	}
}
