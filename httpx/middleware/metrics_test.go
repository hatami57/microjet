package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMetricsRecordsRequests(t *testing.T) {
	m := NewMetrics()
	router := gin.New()
	router.Use(m.Middleware())
	router.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/metrics", gin.WrapH(m.Handler()))

	// Generate a request to record.
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "http_requests_total") {
		t.Errorf("metrics output missing http_requests_total:\n%s", body)
	}
	if !strings.Contains(body, `route="/ping"`) {
		t.Errorf("metrics output missing route label for /ping:\n%s", body)
	}
}
