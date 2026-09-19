package httpx

import (
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestServerTrustsOnlyConfiguredProxies(t *testing.T) {
	for _, tc := range []struct {
		proxies    []string
		peer, want string
	}{
		{nil, "192.0.2.1:1234", "192.0.2.1"},
		{[]string{"192.0.2.0/24"}, "192.0.2.1:1234", "198.51.100.2"},
		{[]string{"192.0.2.0/24"}, "203.0.113.1:1234", "203.0.113.1"},
	} {
		s := NewServer(ServerConfig{TrustedProxies: tc.proxies}, slog.Default())
		if err := s.Init(); err != nil {
			t.Fatal(err)
		}
		s.Router.GET("/ip", func(c *gin.Context) { c.String(200, c.ClientIP()) })
		req := httptest.NewRequest("GET", "/ip", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set("X-Forwarded-For", "198.51.100.2")
		w := httptest.NewRecorder()
		s.Router.ServeHTTP(w, req)
		if w.Body.String() != tc.want {
			t.Fatalf("got %s, want %s", w.Body.String(), tc.want)
		}
	}
	s := NewServer(ServerConfig{TrustedProxies: []string{"invalid"}}, slog.Default())
	if err := s.Init(); err == nil {
		t.Fatal("invalid proxy accepted")
	}
}
