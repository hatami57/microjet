package httpx

import (
	"log/slog"
	"testing"
)

func TestInitRejectsIncompleteTLS(t *testing.T) {
	for _, cfg := range []ServerConfig{{CertFile: "cert.pem"}, {KeyFile: "key.pem"}} {
		s := NewServer(cfg, slog.Default())
		if err := s.Init(); err == nil {
			t.Fatal("incomplete TLS configuration accepted")
		}
		if err := s.Start(); err == nil {
			t.Fatal("server started after failed initialization")
		}
	}
}
