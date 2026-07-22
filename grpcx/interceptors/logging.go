package interceptors

import (
	"context"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoggingUnary returns a unary server interceptor that logs one record per RPC
// with the method, resolved gRPC code, duration, and correlation id. Install it
// after RequestID (so the id is present) and outside Error (so it observes the
// translated status code). A code of OK logs at Info; anything else at Error.
//
// minLevel optionally raises the floor for those lines (see
// ServerConfig.LogLevel): "error" logs only failed RPCs. An empty value follows
// the global level.
func LoggingUnary(logger *slog.Logger, minLevel string) grpc.UnaryServerInterceptor {
	log := logx.WithMinLevel(loggerOrDefault(logger), minLevel)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logRPC(ctx, log, info.FullMethod, err, time.Since(start))
		return resp, err
	}
}

// LoggingStream is the streaming twin of LoggingUnary.
func LoggingStream(logger *slog.Logger, minLevel string) grpc.StreamServerInterceptor {
	log := logx.WithMinLevel(loggerOrDefault(logger), minLevel)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		logRPC(ss.Context(), log, info.FullMethod, err, time.Since(start))
		return err
	}
}

func logRPC(ctx context.Context, log *slog.Logger, method string, err error, dur time.Duration) {
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
