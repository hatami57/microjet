package interceptors

import (
	"context"
	"errors"

	"github.com/hatami57/microjet/core/errorx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrorUnary returns a unary server interceptor that translates a handler's
// error into a gRPC status. It maps the six errorx categories to their gRPC
// codes, passes through errors that already carry a status, and reports anything
// else as codes.Internal. When debug is false the detail of an Internal/untyped
// error is redacted, mirroring httpx middleware.Error's production behavior.
func ErrorUnary(debug bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		return resp, TranslateError(err, debug)
	}
}

// ErrorStream is the streaming twin of ErrorUnary.
func ErrorStream(debug bool) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return TranslateError(handler(srv, ss), debug)
	}
}

// TranslateError converts err into a gRPC status error following the mapping
// documented on ErrorUnary. It returns nil for a nil error and leaves an error
// that already carries a status untouched, so it is safe to apply once at the
// edge of the interceptor chain.
func TranslateError(err error, debug bool) error {
	if err == nil {
		return nil
	}

	var v *errorx.Error
	if errors.As(err, &v) {
		msg := v.Message
		if codeForType(v.Type) == codes.Internal && !debug {
			msg = "internal error"
		}
		return status.Error(codeForType(v.Type), msg)
	}

	// An error that already carries a gRPC status (e.g. from a nested RPC or an
	// explicit status.Error in a handler) passes through unchanged.
	if _, ok := status.FromError(err); ok {
		return err
	}

	if debug {
		return status.Error(codes.Internal, err.Error())
	}
	return status.Error(codes.Internal, "internal error")
}

// codeForType maps an errorx category to its gRPC code; unknown/untyped and the
// Internal category both map to codes.Internal.
func codeForType(t errorx.ErrorType) codes.Code {
	switch t {
	case errorx.BadRequestErrorType:
		return codes.InvalidArgument
	case errorx.NotFoundErrorType:
		return codes.NotFound
	case errorx.BusinessErrorType:
		return codes.FailedPrecondition
	case errorx.UnauthorizedErrorType:
		return codes.Unauthenticated
	case errorx.ForbiddenErrorType:
		return codes.PermissionDenied
	default:
		return codes.Internal
	}
}
