// Package interceptors provides the gRPC server and client interceptors that
// make up MicroJet's default grpcx stack: panic recovery, correlation-id
// propagation, per-RPC logging, and errorx-to-status translation. They mirror
// the httpx middleware stack so an app behaves the same over gRPC and HTTP.
//
// Each interceptor ships as a unary/stream pair; grpcx installs them in the
// order recovery -> request-id -> logging -> error (outermost first), but they
// are exported so an application can compose its own chain.
package interceptors

import (
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryUnary returns a unary server interceptor that turns a panic in a
// handler into a codes.Internal error instead of crashing the server, logging
// the panic value and stack. Install it outermost so it also covers the other
// interceptors.
func RecoveryUnary(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(ctx, logger, info.FullMethod, r)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// RecoveryStream is the streaming twin of RecoveryUnary.
func RecoveryStream(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(ss.Context(), logger, info.FullMethod, r)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}

func logPanic(ctx context.Context, logger *slog.Logger, method string, r any) {
	log := loggerOrDefault(logger)
	log.ErrorContext(ctx, "recovered from panic in gRPC handler",
		"method", method,
		"panic", r,
		"stack", string(debug.Stack()),
	)
}
