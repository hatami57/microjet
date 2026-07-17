package grpcx

import (
	"context"
	"testing"
	"time"

	"github.com/hatami57/microjet/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

func TestDialConnectsAndCalls(t *testing.T) {
	srv, _ := startTestServer(t)
	conn, err := Dial(srv.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}); err != nil {
		t.Fatalf("Check over Dial connection: %v", err)
	}
}

// TestDialPropagatesRequestIDEndToEnd proves the full round-trip: the client
// interceptor injects the context's correlation id into outgoing metadata, the
// server's request-id interceptor reads it, and echoes it back in the trailer.
func TestDialPropagatesRequestIDEndToEnd(t *testing.T) {
	srv, _ := startTestServer(t)
	conn, err := Dial(srv.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx := core.ContextWithCorrelationID(context.Background(), "cid-e2e")
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var trailer metadata.MD
	if _, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}, grpc.Trailer(&trailer)); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got := trailer.Get("x-request-id"); len(got) == 0 || got[0] != "cid-e2e" {
		t.Fatalf("echoed request id = %v, want [cid-e2e]", got)
	}
}
