package middleware

import (
	"io"
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

// TestTimeoutDropsHandlerHeaders verifies headers a timed-out handler set are
// not sent with the 503 (a stale Content-Length truncated its body), while
// headers set before Timeout are.
func TestTimeoutDropsHandlerHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Header("X-Outer", "kept"); c.Next() })
	r.Use(Timeout(10 * time.Millisecond))
	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Length", "2")
		c.Header("X-Handler", "dropped")
		time.Sleep(40 * time.Millisecond)
		c.String(http.StatusOK, "ok")
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading 503 body: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "timeout") {
		t.Fatalf("response = %d %q, want 503 timeout payload", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Handler"); got != "" {
		t.Errorf("timed-out handler's header leaked into the 503: X-Handler=%q", got)
	}
	if got := resp.Header.Get("X-Outer"); got != "kept" {
		t.Errorf("X-Outer = %q, want kept (set before Timeout)", got)
	}
}

func TestTimeoutCommitsHandlerHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Header("X-Outer", "kept"); c.Next() })
	r.Use(Timeout(50 * time.Millisecond))
	r.GET("/", func(c *gin.Context) {
		c.Header("X-Handler", "set")
		c.String(http.StatusOK, "done")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "done" {
		t.Fatalf("response = %d %q, want 200 done", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Handler"); got != "set" {
		t.Errorf("X-Handler = %q, want set", got)
	}
	if got := rec.Header().Get("X-Outer"); got != "kept" {
		t.Errorf("X-Outer = %q, want kept", got)
	}
}

// TestTimeoutHandlerHeaderWritesDoNotRace keeps a handler writing headers past
// the deadline while the watcher sends the 503. The two used to share one header
// map, which the runtime can abort the process on ("concurrent map writes");
// run under -race to catch a regression reliably.
func TestTimeoutHandlerHeaderWritesDoNotRace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Timeout(time.Millisecond))
	r.GET("/", func(c *gin.Context) {
		for deadline := time.Now().Add(5 * time.Millisecond); time.Now().Before(deadline); {
			c.Header("X-Busy", "1")
		}
	})

	for range 20 {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	}
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
