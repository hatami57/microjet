package interceptors

import (
	"context"
	"errors"
	"testing"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/errorx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var testInfo = &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

func TestRecoveryUnaryTurnsPanicIntoInternal(t *testing.T) {
	interceptor := RecoveryUnary(nil)
	_, err := interceptor(context.Background(), nil, testInfo, func(context.Context, any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
}

func TestRequestIDUnaryUsesIncomingID(t *testing.T) {
	interceptor := RequestIDUnary()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(requestIDKey, "abc-123"))

	var seen string
	_, err := interceptor(ctx, nil, testInfo, func(ctx context.Context, _ any) (any, error) {
		seen = core.CorrelationIDFromContext(ctx)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor error: %v", err)
	}
	if seen != "abc-123" {
		t.Fatalf("correlation id = %q, want abc-123", seen)
	}
}

func TestRequestIDUnaryGeneratesWhenAbsent(t *testing.T) {
	interceptor := RequestIDUnary()

	var seen string
	_, _ = interceptor(context.Background(), nil, testInfo, func(ctx context.Context, _ any) (any, error) {
		seen = core.CorrelationIDFromContext(ctx)
		return nil, nil
	})
	if seen == "" {
		t.Fatal("correlation id was not generated when absent")
	}
}

func TestTranslateErrorMapsCategories(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"bad request", errorx.NewBadRequestError("s", "m"), codes.InvalidArgument},
		{"not found", errorx.NewNotFoundError("s", "m"), codes.NotFound},
		{"business", errorx.NewBusinessError("s", "m"), codes.FailedPrecondition},
		{"unauthorized", errorx.NewUnauthorizedError("s", "m"), codes.Unauthenticated},
		{"forbidden", errorx.NewForbiddenError("s", "m"), codes.PermissionDenied},
		{"internal", errorx.NewInternalError("s", "m"), codes.Internal},
		{"plain", errors.New("boom"), codes.Internal},
		{"nil", nil, codes.OK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TranslateError(tc.err, true)
			if status.Code(got) != tc.want {
				t.Fatalf("code = %v, want %v", status.Code(got), tc.want)
			}
		})
	}
}

func TestTranslateErrorRedactsInternalWithoutDebug(t *testing.T) {
	// A non-internal typed error keeps its user-facing message.
	prod := TranslateError(errorx.NewNotFoundError("User", "user not found"), false)
	if got := status.Convert(prod).Message(); got != "user not found" {
		t.Fatalf("message = %q, want the typed message preserved", got)
	}

	// An internal error is redacted unless debug is on.
	redacted := TranslateError(errorx.NewInternalError("DB", "connection string leaked"), false)
	if got := status.Convert(redacted).Message(); got != "internal error" {
		t.Fatalf("message = %q, want redacted 'internal error'", got)
	}
	verbose := TranslateError(errors.New("dsn=secret"), true)
	if got := status.Convert(verbose).Message(); got != "dsn=secret" {
		t.Fatalf("message = %q, want raw error under debug", got)
	}
}

func TestTranslateErrorPassesThroughStatus(t *testing.T) {
	orig := status.Error(codes.AlreadyExists, "dup")
	got := TranslateError(orig, false)
	if got != orig {
		t.Fatalf("status error was not passed through unchanged: %v", got)
	}
}
