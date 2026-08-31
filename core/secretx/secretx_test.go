package secretx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/hatami57/microjet/core/errorx"
)

const plaintext = "hunter2"

// Every verb a secret plausibly reaches a log or a response through. Each of
// these leaking is a credential in a logging pipeline.
func TestValueRedactsEveryFormattingPath(t *testing.T) {
	v := New(plaintext)

	for _, format := range []string{"%v", "%s", "%q", "%#v", "%+v"} {
		t.Run(format, func(t *testing.T) {
			got := fmt.Sprintf(format, v)
			if strings.Contains(got, plaintext) {
				t.Fatalf("%s printed the secret: %s", format, got)
			}
			if !strings.Contains(got, Redacted) {
				t.Errorf("%s = %s, want it to contain %s", format, got, Redacted)
			}
		})
	}
}

// A secret is most likely to escape as a field of the struct that holds it,
// which is the case a bare Stringer test would miss.
func TestValueRedactsInsideAContainingStruct(t *testing.T) {
	creds := struct {
		Username string
		Password Value
	}{Username: "smtp-user", Password: New(plaintext)}

	for _, format := range []string{"%v", "%+v", "%#v"} {
		if got := fmt.Sprintf(format, creds); strings.Contains(got, plaintext) {
			t.Errorf("%s printed the secret: %s", format, got)
		}
	}
}

func TestValueRedactsThroughSlog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	logger.Info("dialling", "user", "smtp-user", "password", New(plaintext))

	if got := buf.String(); strings.Contains(got, plaintext) {
		t.Fatalf("slog wrote the secret: %s", got)
	}
	if got := buf.String(); !strings.Contains(got, Redacted) {
		t.Errorf("log line = %s, want it to contain %s", got, Redacted)
	}
}

func TestValueRedactsThroughJSON(t *testing.T) {
	out, err := json.Marshal(struct {
		Password Value `json:"password"`
	}{Password: New(plaintext)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(out), plaintext) {
		t.Fatalf("JSON carried the secret: %s", out)
	}
}

func TestValueReveal(t *testing.T) {
	if got := New(plaintext).Reveal(); got != plaintext {
		t.Errorf("Reveal = %q, want %q", got, plaintext)
	}
	if !New("").IsEmpty() {
		t.Error("an empty Value should report itself empty")
	}
	if New(plaintext).IsEmpty() {
		t.Error("a set Value should not report itself empty")
	}
}

type safeResolver struct{}

func (safeResolver) Resolve(context.Context, string) (Value, error) { return Value{}, nil }

func TestGuard(t *testing.T) {
	tests := []struct {
		name         string
		resolver     Resolver
		isProduction bool
		wantErr      bool
	}{
		{"insecure outside production", NewStatic(nil), false, false},
		{"insecure in production", NewStatic(nil), true, true},
		{"real store in production", safeResolver{}, true, false},
		{"env in production", NewEnv(), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Guard(tt.resolver, tt.isProduction)
			if tt.wantErr != (err != nil) {
				t.Fatalf("Guard = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var insecure *InsecureError
				if !errors.As(err, &insecure) {
					t.Errorf("err = %v, want an *InsecureError a caller can distinguish", err)
				}
			}
		})
	}
}

func TestEnvResolve(t *testing.T) {
	t.Setenv("SMTP_PASSWORD", plaintext)
	t.Setenv("EMPTY_SECRET", "")
	env := NewEnv()

	got, err := env.Resolve(t.Context(), EnvRef("SMTP_PASSWORD"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Reveal() != plaintext {
		t.Errorf("Reveal = %q, want %q", got.Reveal(), plaintext)
	}

	// A variable deliberately set to empty is an answer; an unset one is a
	// misconfiguration, and the two must not look alike.
	if _, err := env.Resolve(t.Context(), EnvRef("EMPTY_SECRET")); err != nil {
		t.Errorf("an empty variable should resolve, got %v", err)
	}
	if _, err := env.Resolve(t.Context(), EnvRef("DEFINITELY_NOT_SET")); !errorx.IsNotFoundError(err) {
		t.Errorf("err = %v, want a not-found error", err)
	}
}

// A reference minted by one store must not be resolvable by another: silently
// reading "arn:aws:secretsmanager:..." as an environment variable name would
// turn a misconfiguration into an empty password.
func TestEnvRejectsForeignReferences(t *testing.T) {
	env := NewEnv()

	for _, ref := range []string{"SMTP_PASSWORD", StaticRef("smtp"), "arn:aws:secretsmanager:eu-west-1:1:secret:x", ""} {
		if _, err := env.Resolve(t.Context(), ref); err == nil {
			t.Errorf("Resolve(%q) succeeded, want a rejected reference", ref)
		}
	}
}

func TestStaticRoundTrip(t *testing.T) {
	store := NewStatic(map[string]string{"seeded": "from-config"})

	got, err := store.Resolve(t.Context(), StaticRef("seeded"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Reveal() != "from-config" {
		t.Errorf("Reveal = %q, want %q", got.Reveal(), "from-config")
	}

	ref, err := store.Store(t.Context(), "smtp-acme", New(plaintext))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err = store.Resolve(t.Context(), ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Reveal() != plaintext {
		t.Errorf("Reveal = %q, want %q", got.Reveal(), plaintext)
	}

	// Rotation: storing the same name again replaces the value and keeps the
	// reference, so a row pointing at it does not have to be rewritten.
	rotated, err := store.Store(t.Context(), "smtp-acme", New("new-password"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if rotated != ref {
		t.Errorf("reference changed on rotation: %q then %q", ref, rotated)
	}
	got, _ = store.Resolve(t.Context(), ref)
	if got.Reveal() != "new-password" {
		t.Errorf("Reveal = %q, want the rotated value", got.Reveal())
	}
}

func TestStaticDelete(t *testing.T) {
	store := NewStatic(map[string]string{"smtp": plaintext})
	ref := StaticRef("smtp")

	if err := store.Delete(t.Context(), ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Resolve(t.Context(), ref); !errorx.IsNotFoundError(err) {
		t.Errorf("err = %v, want a not-found error after Delete", err)
	}
	// Cleanup paths run twice more often than anyone expects.
	if err := store.Delete(t.Context(), ref); err != nil {
		t.Errorf("deleting twice should be a no-op, got %v", err)
	}
	if err := store.Delete(t.Context(), "arn:aws:secretsmanager:eu-west-1:1:secret:x"); err != nil {
		t.Errorf("deleting a foreign reference should be a no-op, got %v", err)
	}
}

func TestStaticStoreRequiresAName(t *testing.T) {
	store := NewStatic(nil)

	if _, err := store.Store(t.Context(), "  ", New(plaintext)); err == nil {
		t.Fatal("expected an error for a blank secret name")
	}
}

func TestStaticSeedIsCopied(t *testing.T) {
	seed := map[string]string{"smtp": plaintext}
	store := NewStatic(seed)

	// The caller's map is often the config struct, which is reused and mutated.
	seed["smtp"] = "changed"

	got, err := store.Resolve(t.Context(), StaticRef("smtp"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Reveal() != plaintext {
		t.Errorf("Reveal = %q, want the seeded value to have been copied", got.Reveal())
	}
}
