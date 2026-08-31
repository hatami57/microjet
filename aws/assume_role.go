package aws

import (
	"context"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hatami57/microjet/core/errorx"
)

// sessionNameMaxLen is the ceiling STS puts on RoleSessionName. Names are often
// built from a tenant identifier, so the limit is easy to cross by accident;
// AssumeRoleConfig reports it up front rather than letting every send fail with
// a ValidationError from STS.
const sessionNameMaxLen = 64

// AssumeRoleOptions describes a role to assume, normally in another AWS account.
//
// The role's trust policy names this application's own principal (see
// CallerIdentity) and, when ExternalID is set, requires that value in the
// sts:ExternalId condition — the standard defence against the confused-deputy
// problem when the role is created by a third party such as a customer.
type AssumeRoleOptions struct {
	// RoleARN is the role to assume. Required.
	RoleARN string
	// ExternalID is the agreed value the role's trust policy demands. It is not
	// a secret from the party that creates the role — they need it verbatim to
	// write the policy — but it must be unguessable by anyone else.
	ExternalID string
	// Region is where the derived clients talk to their service. Empty keeps the
	// region this application is configured with, which is rarely what a
	// cross-account caller wants: the other account's resources live in the
	// other account's region.
	Region string
	// SessionName labels the session in the assumed account's CloudTrail, so
	// make it identify the caller — "myservice-{tenantID}". Empty uses
	// DefaultSessionName. STS allows [\w+=,.@-] up to 64 characters.
	SessionName string
	// Duration is how long each set of temporary credentials lives before the
	// cache renews it. Zero uses the SDK default of 15 minutes; the ceiling is
	// whatever the role's MaxSessionDuration allows.
	Duration time.Duration
}

// DefaultSessionName labels sessions opened by AssumeRoleConfig when
// AssumeRoleOptions.SessionName is empty.
const DefaultSessionName = "microjet"

// AssumeRoleConfig returns an SDK config that authenticates by assuming the role
// in opts, for building clients that act inside another AWS account.
//
// The returned config is a *copy* of DefaultConfig with its credentials and
// region replaced. That copy is the point of this method: overwriting those
// fields on DefaultConfig itself repoints every client this application already
// built — DynamoDB and the platform's own SES included — at the assumed
// account, which is a data-corruption bug that only shows up under load.
//
// The credentials provider is wrapped in a credentials cache, so a config built
// once and kept serves many calls from one AssumeRole. Build it per role and
// hold on to it; building one per request calls STS every time and will be
// throttled.
//
//	cfg, err := aws.Of(app).AssumeRoleConfig(mjaws.AssumeRoleOptions{
//	    RoleARN:     tenant.RoleARN,
//	    ExternalID:  tenant.ExternalID,
//	    Region:      tenant.Region,
//	    SessionName: "notification-" + tenant.ID.String(),
//	})
//	tenantAWS, err := aws.Of(app).Derive(cfg, mjaws.SES)
//
// The STS call itself is deferred: nothing contacts AWS until a client built
// from the returned config makes its first request. A role that does not exist,
// or a trust policy that rejects this caller, therefore surfaces as an error
// from that first request rather than from here.
func (a *AWS) AssumeRoleConfig(opts AssumeRoleOptions) (awssdk.Config, error) {
	roleARN := strings.TrimSpace(opts.RoleARN)
	if roleARN == "" {
		return awssdk.Config{}, errorx.NewBadRequestError("aws", "assume role: RoleARN is required")
	}

	sessionName := strings.TrimSpace(opts.SessionName)
	if sessionName == "" {
		sessionName = DefaultSessionName
	}
	if err := validateSessionName(sessionName); err != nil {
		return awssdk.Config{}, err
	}

	stsClient, err := a.stsClient()
	if err != nil {
		return awssdk.Config{}, err
	}

	provider := stscreds.NewAssumeRoleProvider(stsClient, roleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = sessionName
		if id := strings.TrimSpace(opts.ExternalID); id != "" {
			o.ExternalID = awssdk.String(id)
		}
		if opts.Duration > 0 {
			o.Duration = opts.Duration
		}
	})

	cfg := a.DefaultConfig
	cfg.Credentials = awssdk.NewCredentialsCache(provider)
	if region := strings.TrimSpace(opts.Region); region != "" {
		cfg.Region = region
	}
	return cfg, nil
}

// Derive returns an AWS client that reaches AWS with cfg — typically credentials
// for another account from AssumeRoleConfig — while inheriting this client's
// logger and its [aws] settings, so per-service endpoint overrides and the S3
// path-style flag still apply.
//
// Only the named services get clients; the rest stay nil. Use it instead of
// building an AWS value field by field: a literal silently leaves every field
// this package adds later at its zero value, and leaves the unnamed clients nil
// in a way no compiler notices.
//
// Per-service defaults are copied, not shared, so the caller may adjust them on
// the returned client without disturbing this one:
//
//	tenantAWS, err := base.Derive(cfg, mjaws.SES)
//	tenantAWS.DefaultSES.SenderEmail = tenant.SenderEmail
//
// The returned client is independent: it has its own lazily built STS client,
// so assuming a further role from it chains from cfg's credentials.
func (a *AWS) Derive(cfg awssdk.Config, services ...Service) (*AWS, error) {
	derived := &AWS{
		DefaultConfig:       cfg,
		DefaultS3BucketName: a.DefaultS3BucketName,
		DefaultSQSQueueURL:  a.DefaultSQSQueueURL,
		DefaultSES:          a.DefaultSES,
		Logger:              a.Logger,
		config:              a.config,
		services:            services,
	}
	if err := derived.initClients(); err != nil {
		return nil, err
	}
	return derived, nil
}

// CallerIdentity returns the ARN of the identity this client authenticates as.
//
// It answers the question a cross-account integration starts with: which
// principal should the other account's trust policy name? Reading it at boot
// beats hard-coding it, though a service running under a role whose ARN is not
// the one a trust policy should name (an assumed-role session ARN, for
// instance) still needs the value configured.
func (a *AWS) CallerIdentity(ctx context.Context) (string, error) {
	stsClient, err := a.stsClient()
	if err != nil {
		return "", err
	}
	out, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", errorx.NewInternalError("aws", "get caller identity failed").WithInner(err)
	}
	if out.Arn == nil {
		return "", errorx.NewInternalError("aws", "get caller identity returned no arn")
	}
	return *out.Arn, nil
}

// stsClient returns the STS client for this application's own account, building
// it on first use when Init was not asked for the STS service.
func (a *AWS) stsClient() (*sts.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.STSClient != nil {
		return a.STSClient, nil
	}
	// A zero DefaultConfig means Init never ran. Building a client from it would
	// produce one with no region and no credentials, which fails later with an
	// error that says nothing about the real cause.
	if a.DefaultConfig.Credentials == nil {
		return nil, errorx.NewInternalError("aws",
			"sts: client is not initialized; install aws.Module or call Init first")
	}
	a.STSClient = sts.NewFromConfig(a.DefaultConfig)
	return a.STSClient, nil
}

// validateSessionName rejects a RoleSessionName STS would reject, naming the
// rule that was broken.
func validateSessionName(name string) error {
	if len(name) > sessionNameMaxLen {
		return errorx.NewBadRequestError("aws",
			"assume role: SessionName is longer than the 64 characters STS allows",
			"length", len(name))
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '+', r == '=', r == ',', r == '.', r == '@', r == '-':
		default:
			return errorx.NewBadRequestError("aws",
				"assume role: SessionName contains a character STS rejects; allowed are letters, digits and _+=,.@-",
				"character", string(r))
		}
	}
	return nil
}
