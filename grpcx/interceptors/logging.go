package interceptors

import (
	"context"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoggingUnary returns a unary server interceptor that logs one record per RPC
// with the method, resolved gRPC code, duration, and correlation id. Install it
// after RequestID (so the id is present) and outside Error (so it observes the
// translated status code). A code of OK logs at Info; anything else at Error.
func LoggingUnary(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logRPC(ctx, logger, info.FullMethod, err, time.Since(start))
		return resp, err
	}
}

// LoggingStream is the streaming twin of LoggingUnary.
func LoggingStream(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		logRPC(ss.Context(), logger, info.FullMethod, err, time.Since(start))
		return err
	}
}

func logRPC(ctx context.Context, logger *slog.Logger, method string, err error, dur time.Duration) {
	log := loggerOrDefault(logger)
	code := status.Code(err)
	attrs := []any{
		"method", method,
		"code", code.String(),
		"duration_ms", float64(dur.Microseconds()) / 1000.0,
		"request_id", core.CorrelationIDFromContext(ctx),
	}
	if code == codes.OK {
		log.InfoContext(ctx, "gRPC request", attrs...)
		return
	}
	log.ErrorContext(ctx, "gRPC request", append(attrs, "error", err.Error())...)
}
