package interceptors

import (
	"context"

	"github.com/hatami57/microjet/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// RequestIDUnaryClient returns a unary client interceptor that copies the
// correlation id from the outgoing context into request metadata, so a call made
// from within a handler carries the same id the server assigned — the client-side
// twin of RequestIDUnary. It is a no-op when the context carries no id.
func RequestIDUnaryClient() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(propagateRequestID(ctx), method, req, reply, cc, opts...)
	}
}

// RequestIDStreamClient is the streaming twin of RequestIDUnaryClient.
func RequestIDStreamClient() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(propagateRequestID(ctx), desc, cc, method, opts...)
	}
}

// propagateRequestID appends the context's correlation id to the outgoing
// metadata, returning ctx unchanged when there is none.
func propagateRequestID(ctx context.Context) context.Context {
	id := core.CorrelationIDFromContext(ctx)
	if id == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, requestIDKey, id)
}
