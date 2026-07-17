// Command grpcx demonstrates MicroJet's managed gRPC server (grpcx): install the
// module, register a service from a Setup hook, and get the same operational
// surface as httpx — recovery/request-id/logging interceptors, an errorx→status
// translator, a health service, and a Dial helper that propagates the request id.
//
// It starts the server in-process on an ephemeral port and calls it with
// grpcx.Dial, so it runs offline:
//
//	go run .
//
// See greeter.go for the (normally protoc-generated) service plumbing.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hatami57/microjet/grpcx"
	"github.com/hatami57/microjet/host"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func main() {
	// Register the JSON codec used by both the server and the client (see
	// greeter.go for why this example is not protobuf).
	encoding.RegisterCodec(jsonCodec{})

	// Build the app: install grpcx on an ephemeral loopback port (port 0 lets the
	// OS choose, so the example never collides with a running server), and
	// register the Greeter from a Setup hook — which runs after init but before
	// the server begins serving.
	app := host.MustNew(
		host.WithConfigValue("grpc.host", "127.0.0.1"),
		host.WithConfigValue("grpc.port", 0),
	).WithModule(grpcx.Module()).
		Setup(func(a *host.App) error {
			grpcx.Of(a).Server().RegisterService(&greeterServiceDesc, greeter{})
			return nil
		})

	// Start brings the app up without blocking; Shutdown drains it at the end.
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		panic(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(shutdownCtx)
	}()

	// Dial the server the grpcx way: the client interceptors propagate the
	// request id and (when otelx is on) trace context. The JSON content-subtype
	// selects our codec for every call.
	addr := grpcx.Of(app).Addr()
	conn, err := grpcx.Dial(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")),
	)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// 1. A successful call.
	var reply HelloReply
	if err := conn.Invoke(ctx, "/example.Greeter/SayHello", &HelloRequest{Name: "Ada"}, &reply); err != nil {
		panic(err)
	}
	fmt.Println("== success ==")
	fmt.Printf("  reply: %q\n\n", reply.Message)

	// 2. A handler errorx error is translated to the matching gRPC status code.
	fmt.Println("== errorx -> gRPC status ==")
	for _, name := range []string{"", "nobody"} {
		err := conn.Invoke(ctx, "/example.Greeter/SayHello", &HelloRequest{Name: name}, &HelloReply{})
		st, _ := status.FromError(err)
		fmt.Printf("  name=%-7q -> code=%-16s message=%q\n", name, st.Code(), st.Message())
	}
	fmt.Printf("  (BadRequest->%s, NotFound->%s)\n\n", codes.InvalidArgument, codes.NotFound)

	// 3. The server echoes the correlation id in the response trailer.
	var trailer metadata.MD
	_ = conn.Invoke(ctx, "/example.Greeter/SayHello", &HelloRequest{Name: "Grace"}, &reply, grpc.Trailer(&trailer))
	fmt.Println("== request id ==")
	fmt.Printf("  trailer x-request-id: %v\n\n", trailer.Get("x-request-id"))

	// 4. The health service reports readiness (wired to the app's readiness
	// checks), so a gRPC load balancer can route on it.
	health, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		panic(err)
	}
	fmt.Println("== health ==")
	fmt.Printf("  status: %s\n", health.Status)
}
