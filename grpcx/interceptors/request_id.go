package interceptors

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// requestIDKey is the metadata key carrying the correlation id, the lowercased
// form of core.CorrelationIDHeader (gRPC metadata keys are always lowercase).
var requestIDKey = strings.ToLower(core.CorrelationIDHeader)

// RequestIDUnary returns a unary server interceptor that ensures every RPC
// carries a correlation id: it reads the id from incoming metadata (generating a
// UUID when absent), stores it on the context via core.ContextWithCorrelationID
// so handlers and downstream calls share it, and echoes it back to the caller in
// the response trailer. It is the gRPC twin of httpx middleware.RequestID.
func RequestIDUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = withRequestID(ctx)
		_ = grpc.SetTrailer(ctx, metadata.Pairs(requestIDKey, core.CorrelationIDFromContext(ctx)))
		return handler(ctx, req)
	}
}

// RequestIDStream is the streaming twin of RequestIDUnary. It wraps the stream so
// the handler sees the correlation-id-carrying context.
func RequestIDStream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := withRequestID(ss.Context())
		_ = grpc.SetTrailer(ctx, metadata.Pairs(requestIDKey, core.CorrelationIDFromContext(ctx)))
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

// withRequestID resolves the correlation id from incoming metadata (or mints one)
// and returns a context carrying it.
func withRequestID(ctx context.Context) context.Context {
	id := incomingRequestID(ctx)
	if id == "" {
		id = uuid.NewString()
	}
	return core.ContextWithCorrelationID(ctx, id)
}

// incomingRequestID returns the first correlation id in the incoming metadata, or
// "" when absent.
func incomingRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if vals := md.Get(requestIDKey); len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// wrappedStream overrides ServerStream.Context so downstream handlers observe the
// enriched context; grpc.ServerStream otherwise exposes the original.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

// loggerOrDefault returns logger, or slog.Default() when nil, so interceptors are
// safe to construct without an explicit logger.
func loggerOrDefault(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}
