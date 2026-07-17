package grpcx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startTestServer boots a Server on an ephemeral loopback port and returns it
// with a client connection; both are cleaned up via t.Cleanup.
func startTestServer(t *testing.T) (*Server, *grpc.ClientConn) {
	t.Helper()
	srv := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0}, testLogger())
	if err := srv.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	conn, err := grpc.NewClient(srv.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return srv, conn
}

func TestServerHealthReflectsReadiness(t *testing.T) {
	srv, conn := startTestServer(t)
	hc := grpc_health_v1.NewHealthClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// The initial status published by Start (no failing checks) is SERVING.
	resp, err := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want SERVING", resp.Status)
	}

	// Flipping readiness off (as the host does at shutdown) reports NOT_SERVING
	// immediately, without waiting for the poll interval.
	srv.SetReady(false)
	resp, err = hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check after SetReady(false): %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status = %v, want NOT_SERVING", resp.Status)
	}
}

func TestServerHealthFollowsFailingCheck(t *testing.T) {
	srv, conn := startTestServer(t)
	var healthy atomic.Bool
	healthy.Store(true)
	srv.AddReadinessCheck("dep", func(context.Context) error {
		if healthy.Load() {
			return nil
		}
		return errors.New("dependency down")
	})

	hc := grpc_health_v1.NewHealthClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Drive an update directly rather than waiting the poll interval.
	srv.updateHealth()
	if resp, _ := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{}); resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want SERVING while dep healthy", resp.Status)
	}

	healthy.Store(false)
	srv.updateHealth()
	if resp, _ := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{}); resp.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status = %v, want NOT_SERVING after dep failed", resp.Status)
	}
}

func TestServerEchoesRequestIDTrailer(t *testing.T) {
	_, conn := startTestServer(t)
	hc := grpc_health_v1.NewHealthClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var trailer metadata.MD
	if _, err := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{}, grpc.Trailer(&trailer)); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got := trailer.Get("x-request-id"); len(got) == 0 || got[0] == "" {
		t.Fatalf("trailer x-request-id = %v, want a generated id", got)
	}
}

func TestServerConfigDefaults(t *testing.T) {
	srv := NewServer(ServerConfig{}, testLogger())
	if err := srv.ReadConfig(mapReader{}); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if srv.config.Host != "0.0.0.0" || srv.config.Port != 9090 {
		t.Fatalf("defaults = %s:%d, want 0.0.0.0:9090", srv.config.Host, srv.config.Port)
	}
}

// mapReader is a minimal configx.Reader that only honors SetDefault, enough to
// exercise ReadConfig's default wiring without a config file.
type mapReader map[string]any

func (m mapReader) SetDefault(key string, value any) { m[key] = value }
func (m mapReader) Read(key string, dest any) error {
	if s, ok := dest.(*ServerConfig); ok {
		if v, ok := m[key+".host"].(string); ok {
			s.Host = v
		}
		if v, ok := m[key+".port"].(int); ok {
			s.Port = v
		}
	}
	return nil
}
func (m mapReader) ReadMap(string) map[string]any { return nil }
func (m mapReader) ReadAll(any) error             { return nil }
