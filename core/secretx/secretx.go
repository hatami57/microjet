// Package secretx separates a secret's storage from its use: application data
// holds a reference, a Resolver turns that reference into the value, and the
// value travels in a type that refuses to print itself.
//
// The discipline it enforces is that a credential belongs in a secret store,
// while the row, document or config that needs it holds only a pointer to it.
// A database dump, a log line or an API response then leaks a reference, which
// is worth nothing without the permission to resolve it.
package secretx

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Redacted is what a Value prints as. It is deliberately conspicuous: seeing it
// in a log is the signal that the redaction worked, not that data was lost.
const Redacted = "[REDACTED]"

// Value holds a secret and will not reveal it by accident.
//
// It prints as [REDACTED] through fmt, slog and encoding/json, so a secret
// cannot reach a log line, an error message or a response body by being
// interpolated into one. Reveal is the only way out, which makes every place
// that genuinely needs the plaintext greppable.
//
//	password, err := resolver.Resolve(ctx, cfg.PasswordRef)
//	if err != nil {
//	    return err
//	}
//	logger.Info("dialling", "user", cfg.Username, "password", password) // [REDACTED]
//	client.Auth(cfg.Username, password.Reveal())
//
// The zero Value is an empty secret. Values are comparable, so a Value can be a
// map key or be compared with ==, and copying one copies the secret.
type Value struct {
	s string
}

// New wraps a plaintext secret in a Value.
func New(s string) Value { return Value{s: s} }

// Reveal returns the plaintext. Call it at the point of use — passing it to the
// client that needs it — and do not hold the result in a struct that something
// might later log or serialise.
func (v Value) Reveal() string { return v.s }

// IsEmpty reports whether the Value holds nothing, which is how a missing
// secret differs from one that resolved to the empty string.
func (v Value) IsEmpty() bool { return v.s == "" }

// String implements fmt.Stringer, covering %v, %s and %q.
func (v Value) String() string { return Redacted }

// GoString implements fmt.GoStringer, covering %#v — the verb a debugging
// print reaches for, and the one that would otherwise dump the struct field.
func (v Value) GoString() string { return Redacted }

// LogValue implements slog.LogValuer, so a Value passed to a structured logger
// as an attribute is redacted rather than formatted.
func (v Value) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalJSON implements json.Marshaler, so a Value on a response struct
// serialises as [REDACTED] instead of the secret.
func (v Value) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

// Compile-time proof that the redacting methods satisfy the interfaces fmt,
// slog and encoding/json look for. Losing one of these silently un-redacts
// that path.
var (
	_ slog.LogValuer = Value{}
	_ json.Marshaler = Value{}
)

// Resolver turns a reference into the secret it points at.
//
// A reference is opaque to its holder: only the Resolver that issued it knows
// whether it is an ARN, an environment variable name or a path. Implementations
// should return a not-found error for an unknown reference, distinguishable
// from a failure to reach the store — the caller usually treats the first as a
// misconfiguration and the second as worth retrying.
type Resolver interface {
	Resolve(ctx context.Context, ref string) (Value, error)
}

// Storer writes secrets and hands back the references to them. Splitting it
// from Resolver keeps the read path — the one every request takes — free of
// permissions it does not need.
type Storer interface {
	// Store writes value under a name of the caller's choosing and returns the
	// reference to record. Storing the same name again replaces the value and
	// returns a reference to it.
	Store(ctx context.Context, name string, value Value) (ref string, err error)
	// Delete removes the secret a reference points at. Deleting an unknown
	// reference is not an error, so cleaning up twice is safe.
	Delete(ctx context.Context, ref string) error
}

// ReadWriter is the pair of them, for a store that does both.
type ReadWriter interface {
	Resolver
	Storer
}

// Insecure is implemented by a Resolver that does not actually protect what it
// holds — one that keeps secrets in memory, in plain config, or alongside the
// data that references them. Such a resolver is useful for tests and local
// development and has no business in production.
type Insecure interface {
	// Insecure reports that this resolver must not hold production secrets.
	Insecure() bool
}

// Guard reports whether r may be used, given whether this is production.
//
// Call it once at startup, so an insecure resolver left in place by a bad
// deployment stops the process rather than quietly protecting nothing:
//
//	if err := secretx.Guard(resolver, app.Config.App.IsProduction()); err != nil {
//	    return err
//	}
//
// A resolver that does not implement Insecure is assumed to be safe, so a real
// secret store needs no cooperation to pass.
func Guard(r Resolver, isProduction bool) error {
	if !isProduction {
		return nil
	}
	if insecure, ok := r.(Insecure); ok && insecure.Insecure() {
		return &InsecureError{Resolver: r}
	}
	return nil
}

// InsecureError reports an insecure Resolver reaching production. It is a
// distinct type so a caller can tell this apart from an ordinary startup
// failure — it is a deployment mistake, not a broken dependency.
type InsecureError struct {
	Resolver Resolver
}

func (e *InsecureError) Error() string {
	return "secretx: refusing to start: this secret resolver does not protect " +
		"secrets and app.environment is production"
}
