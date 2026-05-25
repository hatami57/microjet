package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestBuilderIsImmutable(t *testing.T) {
	base := ErrBadRequest
	enriched := base.WithSubject("email").WithCode(42).WithMessage("bad email")

	if base.Subject != "General" {
		t.Fatalf("base sentinel mutated: subject = %q, want %q", base.Subject, "General")
	}
	if base.Code != 0 {
		t.Fatalf("base sentinel mutated: code = %d, want 0", base.Code)
	}
	if enriched.Subject != "email" || enriched.Code != 42 || enriched.Message != "bad email" {
		t.Fatalf("enriched error not built correctly: %+v", enriched)
	}
}

func TestTypePredicates(t *testing.T) {
	cases := []struct {
		err  error
		pred func(error) bool
		want bool
	}{
		{ErrBadRequest, IsBadRequestError, true},
		{ErrBadRequest, IsNotFoundError, false},
		{ErrNotFound, IsNotFoundError, true},
		{ErrBusiness, IsBusinessError, true},
		{ErrUnauthorized, IsUnauthorizedError, true},
		{ErrForbidden, IsForbiddenError, true},
		{ErrInternal, IsInternalError, true},
		{errors.New("plain"), IsInternalError, false},
	}
	for i, c := range cases {
		if got := c.pred(c.err); got != c.want {
			t.Errorf("case %d: predicate = %v, want %v", i, got, c.want)
		}
	}
}

func TestWrappedErrorIsDetected(t *testing.T) {
	wrapped := fmt.Errorf("service layer: %w", ErrNotFound.WithSubject("User"))
	if !IsNotFoundError(wrapped) {
		t.Fatal("wrapped NotFound error not detected through errors.As")
	}
	appErr := GetError(wrapped)
	if appErr == nil || appErr.Subject != "User" {
		t.Fatalf("GetError = %+v, want subject User", appErr)
	}
}

func TestUnwrapReturnsInner(t *testing.T) {
	inner := errors.New("db exploded")
	err := ErrInternal.WithInner(inner)
	if !errors.Is(err, inner) {
		t.Fatal("errors.Is did not match inner error")
	}
}
