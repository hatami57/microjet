// Command errors demonstrates MicroJet's structured error system (core/errorx):
// the six error categories, builder-pattern enrichment, JSON shape, sentinel
// matching with errors.Is, and typed extraction with errorx.GetError.
//
// It is a plain program — no HTTP server — so you can see exactly what each
// helper produces. Run it with:
//
//	go run .
package main

import (
	"errors"
	"fmt"

	"github.com/hatami57/microjet/core/errorx"
)

func main() {
	// 1. The six categories. Each constructor takes a subject (what the error is
	// about) and a human-readable message. The category drives the HTTP status
	// when the error reaches the httpx error middleware (BadRequest→400,
	// NotFound→404, Business→409, Unauthorized→401, Forbidden→403, Internal→500).
	fmt.Println("== categories ==")
	for _, e := range []*errorx.Error{
		errorx.NewBadRequestError("User", "name is required"),
		errorx.NewNotFoundError("User", "user does not exist"),
		errorx.NewBusinessError("Order", "order already shipped"),
		errorx.NewUnauthorizedError("Session", "token expired"),
		errorx.NewForbiddenError("Project", "not a member"),
		errorx.NewInternalError("DB", "query failed"),
	} {
		fmt.Printf("  %-12s %s\n", e.Type, e.Error())
	}

	// 2. Builder enrichment. Every With* method returns a copy, so a sentinel can
	// be specialized without mutating the shared value (see step 4).
	// WithMessage replaces the message; its trailing key-value pairs are merged
	// into Params exactly like WithParams (it is not a printf — pre-format the
	// message yourself if you need interpolation).
	enriched := errorx.ErrNotFound.
		WithSubject("Invoice").
		WithMessage(fmt.Sprintf("invoice %d not found", 42), "invoiceID", 42).
		WithParams("tenant", "acme").
		WithCode(4404)
	fmt.Println("\n== enriched ==")
	fmt.Printf("  %s\n  params=%v code=%d\n", enriched.Error(), enriched.Params, enriched.Code)

	// 3. JSON shape — this is what a client sees through the HTTP error middleware.
	asJSON, _ := enriched.MarshalJSON()
	fmt.Println("\n== json ==")
	fmt.Printf("  %s\n", asJSON)

	// 4. Sentinel matching. errors.Is matches by category, so you can branch on
	// "is this a not-found?" without caring about the subject/message.
	fmt.Println("\n== errors.Is (by category) ==")
	fmt.Printf("  enriched is ErrNotFound: %v\n", errors.Is(enriched, errorx.ErrNotFound))
	fmt.Printf("  enriched is ErrInternal: %v\n", errors.Is(enriched, errorx.ErrInternal))

	// 5. Wrapping a low-level cause with WithInner. The inner error is unwrapped
	// by errors.Is/As but hidden from the JSON response unless debug is on.
	wrapped := errorx.NewInternalError("DB", "saving user failed").
		WithInner(fmt.Errorf("connection refused"))
	fmt.Println("\n== wrapping ==")
	fmt.Printf("  %s\n", wrapped.Error())

	// 6. Typed extraction. errorx.GetError pulls the *errorx.Error back out of any
	// error chain so handlers can read its category, subject, and params.
	var plain error = wrapped
	if e := errorx.GetError(plain); e != nil {
		fmt.Println("\n== extraction ==")
		fmt.Printf("  recovered type=%s subject=%s\n", e.Type, e.Subject)
	}
}
