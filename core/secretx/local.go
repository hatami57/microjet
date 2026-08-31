package secretx

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/hatami57/microjet/core/errorx"
)

// EnvPrefix is the reference prefix Env answers to.
const EnvPrefix = "env:"

// Env resolves a reference of the form "env:NAME" from the process
// environment. It is a legitimate production mechanism — it is how a container
// orchestrator or a secrets operator usually delivers a credential — so it is
// not marked insecure.
//
// It is read-only: there is no way to write an environment variable that
// outlives the process, so Env implements Resolver and not Storer. Pair it with
// a real store when the application also has to create secrets.
type Env struct{}

// NewEnv returns a Resolver backed by the process environment.
func NewEnv() *Env { return &Env{} }

// Resolve reads the environment variable named by ref, which must carry the
// "env:" prefix so that a reference issued by a different store is rejected
// rather than silently looked up in the environment.
func (e *Env) Resolve(_ context.Context, ref string) (Value, error) {
	name, ok := strings.CutPrefix(ref, EnvPrefix)
	if !ok {
		return Value{}, errorx.NewBadRequestError("secretx",
			"reference is not an environment reference", "want_prefix", EnvPrefix)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Value{}, errorx.NewBadRequestError("secretx", "environment reference names no variable")
	}
	// An unset variable and one set to "" are different: the first is a
	// misconfiguration, the second is a deliberate empty secret.
	v, ok := os.LookupEnv(name)
	if !ok {
		return Value{}, errorx.NewNotFoundError("secretx", "environment variable is not set", "name", name)
	}
	return New(v), nil
}

// EnvRef returns the reference that resolves to environment variable name.
func EnvRef(name string) string { return EnvPrefix + name }

// StaticPrefix is the reference prefix Static issues and answers to.
const StaticPrefix = "static:"

// Static keeps secrets in memory. It exists for tests and for running locally
// without a secret store, and it reports itself as Insecure so that Guard stops
// a deployment that reaches production still using it.
//
// Stored secrets do not outlive the process, which is the other reason not to
// mistake it for a store: a reference recorded in a database today resolves to
// nothing after a restart.
type Static struct {
	mu      sync.RWMutex
	secrets map[string]string
}

// NewStatic returns an in-memory store seeded with the given name/value pairs,
// each reachable at StaticRef(name).
func NewStatic(seed map[string]string) *Static {
	s := &Static{secrets: make(map[string]string, len(seed))}
	for name, value := range seed {
		s.secrets[name] = value
	}
	return s
}

// Insecure implements the Insecure interface, and always reports true.
func (s *Static) Insecure() bool { return true }

// Resolve returns the secret stored under ref.
func (s *Static) Resolve(_ context.Context, ref string) (Value, error) {
	name, ok := strings.CutPrefix(ref, StaticPrefix)
	if !ok {
		return Value{}, errorx.NewBadRequestError("secretx",
			"reference is not a static reference", "want_prefix", StaticPrefix)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.secrets[name]
	if !ok {
		return Value{}, errorx.NewNotFoundError("secretx", "no secret under that reference")
	}
	return New(v), nil
}

// Store writes value under name, replacing whatever was there.
func (s *Static) Store(_ context.Context, name string, value Value) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errorx.NewBadRequestError("secretx", "secret name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.secrets[name] = value.Reveal()
	return StaticRef(name), nil
}

// Delete removes the secret ref points at, and is a no-op for one that is
// already gone.
func (s *Static) Delete(_ context.Context, ref string) error {
	name, ok := strings.CutPrefix(ref, StaticPrefix)
	if !ok {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.secrets, name)
	return nil
}

// StaticRef returns the reference that resolves to the static secret name.
func StaticRef(name string) string { return StaticPrefix + name }

// Compile-time proof that the local implementations satisfy the interfaces
// callers wire them in as.
var (
	_ Resolver   = (*Env)(nil)
	_ ReadWriter = (*Static)(nil)
	_ Insecure   = (*Static)(nil)
)
