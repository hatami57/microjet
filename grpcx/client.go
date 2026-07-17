package grpcx

import (
	"github.com/hatami57/microjet/grpcx/interceptors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

// Dial creates a client connection to target with MicroJet's client
// interceptors installed: correlation-id propagation (the request id from the
// calling context is carried into outgoing metadata, so a downstream RPC keeps
// the caller's id) and the otelgrpc client stats handler for trace propagation
// (a no-op until otelx installs a tracer provider). It is the gRPC parity for
// what httpx.Client provides over HTTP.
//
// Like grpc.NewClient, Dial does not require the server to be reachable and does
// not dial eagerly; the connection is established on the first RPC. Callers must
// supply transport credentials among opts (e.g.
// grpc.WithTransportCredentials(insecure.NewCredentials()) for plaintext), and
// any extra opts are appended after the defaults so they can override them.
func Dial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	base := []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(interceptors.RequestIDUnaryClient()),
		grpc.WithChainStreamInterceptor(interceptors.RequestIDStreamClient()),
	}
	return grpc.NewClient(target, append(base, opts...)...)
}
