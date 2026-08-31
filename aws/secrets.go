package aws

import (
	"context"
	"errors"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/secretx"
)

// SecretStore is a secretx.ReadWriter backed by AWS Secrets Manager: the
// application records the ARN a Store call returns, and the secret itself never
// leaves AWS except through Resolve.
//
// Resolve is a network call, and Secrets Manager is rate limited per account.
// A secret read on a hot path belongs behind a cache — the value is stable
// between rotations, so a short TTL costs nothing and removes the call from
// every request.
type SecretStore struct {
	client     *secretsmanager.Client
	namePrefix string
	force      bool
}

// SecretStoreOption customizes a SecretStore.
type SecretStoreOption func(*SecretStore)

// WithSecretNamePrefix prepends prefix to the name of every secret Store
// creates, which is how one account holds several environments' secrets
// without collision ("staging/", "prod/"). It does not affect Resolve: a
// reference already carries the full name or ARN.
func WithSecretNamePrefix(prefix string) SecretStoreOption {
	return func(s *SecretStore) { s.namePrefix = prefix }
}

// WithForceDelete makes Delete remove a secret immediately instead of
// scheduling it for deletion after a recovery window.
//
// The default is the recovery window, because a deleted credential is
// unrecoverable and deletions are usually someone's mistake. The cost of that
// default is that a secret's name stays taken while the window runs, so
// creating the same name again fails until it elapses — if the application
// deletes and recreates a secret under a stable name (per tenant, per
// integration), it wants this option.
func WithForceDelete() SecretStoreOption {
	return func(s *SecretStore) { s.force = true }
}

// SecretStore returns a secret store backed by this client's account and
// region. Request the SecretsManager service from Init, or the client is built
// on demand from DefaultConfig.
//
//	store := aws.Of(app).SecretStore(mjaws.WithSecretNamePrefix("prod/"))
//	ref, err := store.Store(ctx, "smtp/"+tenantID.String(), secretx.New(password))
//
// Each call returns a new wrapper over the same underlying client, so options
// can differ per use without duplicating connections.
func (a *AWS) SecretStore(opts ...SecretStoreOption) *SecretStore {
	a.mu.Lock()
	if a.SecretsManagerClient == nil {
		a.SecretsManagerClient = secretsmanager.NewFromConfig(a.DefaultConfig)
	}
	client := a.SecretsManagerClient
	a.mu.Unlock()

	store := &SecretStore{client: client}
	for _, opt := range opts {
		opt(store)
	}
	return store
}

// Resolve reads the current value of the secret named by ref, which is either
// the secret's name or its ARN.
func (s *SecretStore) Resolve(ctx context.Context, ref string) (secretx.Value, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return secretx.Value{}, errorx.NewBadRequestError("aws", "secret reference is required")
	}

	out, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &ref})
	if err != nil {
		// A reference pointing at nothing is a misconfiguration the caller can
		// report; every other failure is worth retrying, so the two must not
		// look alike.
		var notFound *smtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return secretx.Value{}, errorx.NewNotFoundError("aws", "no such secret").WithInner(err)
		}
		return secretx.Value{}, errorx.NewInternalError("aws", "get secret value failed").WithInner(err)
	}

	// A secret written through the console as binary comes back in the other
	// field; reading it as text is better than reporting an empty password.
	switch {
	case out.SecretString != nil:
		return secretx.New(*out.SecretString), nil
	case out.SecretBinary != nil:
		return secretx.New(string(out.SecretBinary)), nil
	default:
		return secretx.Value{}, errorx.NewNotFoundError("aws", "secret has no value")
	}
}

// Store writes value under name and returns the secret's ARN to record.
//
// It creates the secret the first time and adds a new version after that, so
// the returned reference is stable across rotations and a row holding it never
// has to be rewritten.
func (s *SecretStore) Store(ctx context.Context, name string, value secretx.Value) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errorx.NewBadRequestError("aws", "secret name is required")
	}
	full := s.namePrefix + name
	plaintext := value.Reveal()

	out, err := s.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         &full,
		SecretString: &plaintext,
	})
	if err == nil {
		return awssdk.ToString(out.ARN), nil
	}

	var exists *smtypes.ResourceExistsException
	if !errors.As(err, &exists) {
		return "", errorx.NewInternalError("aws", "create secret failed").WithInner(err)
	}

	put, err := s.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     &full,
		SecretString: &plaintext,
	})
	if err != nil {
		return "", errorx.NewInternalError("aws", "put secret value failed").WithInner(err)
	}
	return awssdk.ToString(put.ARN), nil
}

// Delete removes the secret ref points at, scheduling it for deletion after the
// account's recovery window unless WithForceDelete was set. A reference that
// points at nothing is not an error, so cleaning up twice is safe.
func (s *SecretStore) Delete(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}

	input := &secretsmanager.DeleteSecretInput{SecretId: &ref}
	if s.force {
		input.ForceDeleteWithoutRecovery = awssdk.Bool(true)
	}

	if _, err := s.client.DeleteSecret(ctx, input); err != nil {
		var notFound *smtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return errorx.NewInternalError("aws", "delete secret failed").WithInner(err)
	}
	return nil
}

// Compile-time proof that the store satisfies the interfaces it is wired in as.
var _ secretx.ReadWriter = (*SecretStore)(nil)
