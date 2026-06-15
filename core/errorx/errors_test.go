package errorx

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

func TestIsMatchesByCategory(t *testing.T) {
	// An enriched NotFound error matches the NotFound sentinel by category...
	notFound := NewNotFoundError("User", "user not found").WithCode(404)
	if !errors.Is(notFound, ErrNotFound) {
		t.Error("errors.Is(notFound, ErrNotFound) = false, want true")
	}
	// ...and does not match a different category.
	if errors.Is(notFound, ErrBadRequest) {
		t.Error("errors.Is(notFound, ErrBadRequest) = true, want false")
	}
	// Matches through wrapping too.
	wrapped := fmt.Errorf("layer: %w", notFound)
	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("errors.Is(wrapped, ErrNotFound) = false, want true")
	}
}

func TestIsCustomSentinelMatchesNarrowly(t *testing.T) {
	// A custom sentinel that pins Subject and Code matches only those errors.
	sentinel := NewBusinessError("Wallet", "insufficient funds").WithCode(1001)

	match := NewBusinessError("Wallet", "nope").WithCode(1001)
	if !errors.Is(match, sentinel) {
		t.Error("errors.Is should match same Type+Subject+Code")
	}
	otherCode := NewBusinessError("Wallet", "nope").WithCode(1002)
	if errors.Is(otherCode, sentinel) {
		t.Error("different Code should not match a Code-pinned sentinel")
	}
	otherSubject := NewBusinessError("Order", "nope").WithCode(1001)
	if errors.Is(otherSubject, sentinel) {
		t.Error("different Subject should not match a Subject-pinned sentinel")
	}
}

func TestIsDoesNotMatchPlainError(t *testing.T) {
	if errors.Is(ErrInternal, errors.New("plain")) {
		t.Error("typed error should not match an unrelated plain error")
	}
}
