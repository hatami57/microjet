package main

import (
	"context"
	"encoding/json"

	"github.com/hatami57/microjet/core/errorx"
	"google.golang.org/grpc"
)

// A real gRPC service uses Protocol Buffers with protoc-generated code. To keep
// this example runnable with `go run .` and no protoc toolchain, it uses plain
// Go structs, a tiny JSON codec, and a hand-written grpc.ServiceDesc — the exact
// shape protoc-gen-go-grpc produces. The grpcx wiring (module, interceptors,
// health, Dial) is identical either way; only the message encoding differs.

// HelloRequest / HelloReply are the request and response messages.
type HelloRequest struct {
	Name string `json:"name"`
}

type HelloReply struct {
	Message string `json:"message"`
}

// GreeterServer is the service interface an implementation satisfies.
type GreeterServer interface {
	SayHello(context.Context, *HelloRequest) (*HelloReply, error)
}

// greeter implements GreeterServer, returning typed errorx errors that the
// grpcx interceptor stack translates into gRPC status codes:
//   - errorx BadRequest  -> codes.InvalidArgument
//   - errorx NotFound    -> codes.NotFound
type greeter struct{}

func (greeter) SayHello(_ context.Context, req *HelloRequest) (*HelloReply, error) {
	switch req.Name {
	case "":
		return nil, errorx.NewBadRequestError("greeter", "name is required")
	case "nobody":
		return nil, errorx.NewNotFoundError("greeter", "no such user", "name", req.Name)
	default:
		return &HelloReply{Message: "Hello, " + req.Name + "!"}, nil
	}
}

// greeterServiceDesc is what protoc-gen-go-grpc would generate for the service.
var greeterServiceDesc = grpc.ServiceDesc{
	ServiceName: "example.Greeter",
	HandlerType: (*GreeterServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "SayHello", Handler: sayHelloHandler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "example",
}

// sayHelloHandler decodes the request and invokes the handler through the
// server's interceptor chain — the standard generated-code pattern.
func sayHelloHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(HelloRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GreeterServer).SayHello(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/example.Greeter/SayHello"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(GreeterServer).SayHello(ctx, req.(*HelloRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// jsonCodec is a minimal gRPC codec so the example needs no protobuf. Both the
// server and the client select it (the client via grpc.CallContentSubtype).
type jsonCodec struct{}

func (jsonCodec) Name() string                       { return "json" }
func (jsonCodec) Marshal(v any) ([]byte, error)      { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
