package aws

import (
	"context"
	"errors"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/secretx"
)

// ParameterStore is a secretx.ReadWriter backed by SSM Parameter Store, holding
// each secret as a SecureString: the application records the parameter's path
// and the plaintext never leaves AWS except through Resolve.
//
// It is interchangeable with [SecretStore] — same interface, same contract — and
// exists because the two services are priced very differently for the same job.
// Parameter Store's standard tier stores secrets for free, where Secrets Manager
// bills per secret per month. What that price buys is rotation via Lambda,
// resource policies for cross-account reads, and cross-region replication; an
// application that stores a credential its user supplied, reads it back itself,
// and rotates it when that user says so is paying for none of them. Reach for
// [SecretStore] when one of those three is actually in use, and for this
// otherwise.
//
// Three differences are worth knowing before choosing:
//
//   - Delete is immediate, where Secrets Manager schedules deletion and holds
//     the name out of use for a recovery window. That makes a stable per-tenant
//     name safe to delete and recreate, which [SecretStore] needs
//     [WithForceDelete] to survive — at the cost of the window's safety net,
//     since a deleted parameter is gone at once.
//   - The standard tier caps a value at 4KB and an account at 10,000 parameters
//     per region. Both are lifted by the advanced tier, which is billed per
//     parameter but still an order of magnitude below Secrets Manager. Tier is
//     not set here, so a parameter is created in whichever tier the account's
//     default tier configuration names: the ceiling is raised in the account,
//     not in this code.
//   - A SecureString is encrypted with KMS, so Resolve needs kms:Decrypt on the
//     key as well as ssm:GetParameter on the path. The key is the AWS managed
//     aws/ssm unless [WithParameterKeyID] names another.
//
// Resolve is a network call, and a secret read on a hot path belongs behind a
// cache exactly as it does with [SecretStore]. Parameter Store's default limit
// is the lower of the two — 40 TPS shared across the account, against Secrets
// Manager's thousands — so caching matters more here, and a burst of cold
// processes is the shape of traffic that finds the limit.
type ParameterStore struct {
	client     *ssm.Client
	namePrefix string
	keyID      string
}

// ParameterStoreOption customizes a ParameterStore.
type ParameterStoreOption func(*ParameterStore)

// WithParameterNamePrefix prepends prefix to the name of every parameter Store
// writes, which is how one account holds several environments' secrets without
// collision ("staging/", "prod/"). It does not affect Resolve: a reference
// already carries the full path.
//
// Because names are a hierarchy, the prefix is also the unit IAM scopes to — a
// policy naming parameter/prod/* covers one environment and no other — so a
// prefix here is worth more than the collision it prevents.
func WithParameterNamePrefix(prefix string) ParameterStoreOption {
	return func(s *ParameterStore) { s.namePrefix = strings.TrimSpace(prefix) }
}

// WithParameterKeyID encrypts SecureString parameters under the named KMS key
// (an ID, alias or ARN) instead of the account's AWS managed aws/ssm key.
//
// The managed key is free and sufficient for most uses. A customer managed key
// costs a flat monthly fee regardless of how many parameters use it, and buys
// what the managed one cannot give: a key policy of your own, rotation on your
// schedule, and the ability to revoke access to every secret at once by
// disabling a single key.
func WithParameterKeyID(keyID string) ParameterStoreOption {
	return func(s *ParameterStore) { s.keyID = strings.TrimSpace(keyID) }
}

// ParameterStore returns a secret store backed by this client's account and
// region. Request the SSM service from Init, or the client is built on demand
// from DefaultConfig.
//
//	store := aws.Of(app).ParameterStore(mjaws.WithParameterNamePrefix("prod/"))
//	ref, err := store.Store(ctx, "smtp/"+tenantID.String(), secretx.New(password))
//
// Each call returns a new wrapper over the same underlying client, so options
// can differ per use without duplicating connections.
func (a *AWS) ParameterStore(opts ...ParameterStoreOption) *ParameterStore {
	a.mu.Lock()
	if a.SSMClient == nil {
		a.SSMClient = ssm.NewFromConfig(a.DefaultConfig)
	}
	client := a.SSMClient
	a.mu.Unlock()

	store := &ParameterStore{client: client}
	for _, opt := range opts {
		opt(store)
	}
	return store
}

// Resolve reads the current value of the parameter at ref, decrypting it.
func (s *ParameterStore) Resolve(ctx context.Context, ref string) (secretx.Value, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return secretx.Value{}, errorx.NewBadRequestError("aws", "parameter reference is required")
	}

	out, err := s.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &ref,
		WithDecryption: awssdk.Bool(true),
	})
	if err != nil {
		// A reference pointing at nothing is a misconfiguration the caller can
		// report; every other failure — including a denied kms:Decrypt, which is
		// a policy that can be fixed without the secret being lost — is worth
		// retrying, so the two must not look alike.
		var notFound *ssmtypes.ParameterNotFound
		var versionNotFound *ssmtypes.ParameterVersionNotFound
		if errors.As(err, &notFound) || errors.As(err, &versionNotFound) {
			return secretx.Value{}, errorx.NewNotFoundError("aws", "no such parameter").WithInner(err)
		}
		return secretx.Value{}, errorx.NewInternalError("aws", "get parameter failed").WithInner(err)
	}

	if out.Parameter == nil || out.Parameter.Value == nil {
		return secretx.Value{}, errorx.NewNotFoundError("aws", "parameter has no value")
	}
	return secretx.New(*out.Parameter.Value), nil
}

// Store writes value as a SecureString at name and returns the full path to
// record.
//
// It creates the parameter the first time and adds a new version after that, so
// the returned reference is stable across rotations and a row holding it never
// has to be rewritten. Parameter Store keeps 100 versions and discards the
// oldest unlabelled one beyond that, which bounds the history rather than
// failing a rotation.
//
// The reference is the parameter's path, not an ARN: PutParameter does not
// return one, and constructing it would mean knowing the account ID and
// partition. A path is a reference a Resolver understands, which is all
// secretx asks of it — and since a path begins with "/" where a Secrets Manager
// reference begins with "arn:", a stored reference still says which store
// issued it.
func (s *ParameterStore) Store(ctx context.Context, name string, value secretx.Value) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errorx.NewBadRequestError("aws", "parameter name is required")
	}
	full := parameterPath(s.namePrefix + name)
	plaintext := value.Reveal()

	input := &ssm.PutParameterInput{
		Name:      &full,
		Value:     &plaintext,
		Type:      ssmtypes.ParameterTypeSecureString,
		Overwrite: awssdk.Bool(true),
	}
	if s.keyID != "" {
		input.KeyId = &s.keyID
	}

	if _, err := s.client.PutParameter(ctx, input); err != nil {
		return "", errorx.NewInternalError("aws", "put parameter failed").WithInner(err)
	}
	return full, nil
}

// Delete removes the parameter ref points at, immediately and without a
// recovery window. A reference that points at nothing is not an error, so
// cleaning up twice is safe.
func (s *ParameterStore) Delete(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}

	if _, err := s.client.DeleteParameter(ctx, &ssm.DeleteParameterInput{Name: &ref}); err != nil {
		var notFound *ssmtypes.ParameterNotFound
		if errors.As(err, &notFound) {
			return nil
		}
		return errorx.NewInternalError("aws", "delete parameter failed").WithInner(err)
	}
	return nil
}

// parameterPath puts a name into the hierarchical form Parameter Store requires:
// a name holding a "/" is rejected unless it starts with one. Prefixing every
// name rather than only the nested ones keeps one shape for all of them, so a
// stored reference can be recognised by its leading "/" whatever it names.
func parameterPath(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "/") {
		return name
	}
	return "/" + name
}

// Compile-time proof that the store satisfies the interfaces it is wired in as.
var _ secretx.ReadWriter = (*ParameterStore)(nil)
