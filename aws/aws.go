// Package aws provides initialized AWS service clients (S3, SQS, DynamoDB, SES),
// wired into the host as a module that reads the [aws] config section.
package aws

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
)

type AWS struct {
	DefaultConfig       awssdk.Config
	DefaultS3BucketName *string
	DefaultSQSQueueURL  *string
	DefaultSES          SESConfig
	Logger              *slog.Logger

	S3Client       *s3.Client
	SQSClient      *sqs.Client
	DynamoDBClient *dynamodb.Client
	SESClient      *sesv2.Client
	// STSClient is built by Init when the STS service is requested, and
	// otherwise on first use by AssumeRoleConfig or CallerIdentity. It always
	// speaks for this application's own account, never an assumed one.
	STSClient *sts.Client
	// SecretsManagerClient is built by Init when the SecretsManager service is
	// requested, and otherwise on first use by SecretStore.
	SecretsManagerClient *secretsmanager.Client

	config   Config
	services []Service

	// mu guards the lazy STSClient build. Every other field is written during
	// Init, before the client is shared.
	mu sync.Mutex
}

// NewAWS returns an AWS client ready to participate in the host service
// lifecycle. services lists which SDK clients (S3, SQS, DynamoDB, SES) to
// initialize on Init.
func NewAWS(logger *slog.Logger, services ...Service) *AWS {
	return &AWS{Logger: logger, services: services}
}

type Service string

const (
	S3       Service = "aws-s3"
	SQS      Service = "aws-sqs"
	DynamoDB Service = "aws-dynamodb"
	SES      Service = "aws-ses"
	// STS builds the client used to assume roles in other accounts. Requesting
	// it is optional: AssumeRoleConfig and CallerIdentity build it on demand.
	STS Service = "aws-sts"
	// SecretsManager builds the client behind SecretStore. Requesting it is
	// optional: SecretStore builds it on demand.
	SecretsManager Service = "aws-secretsmanager"
)

// ReadConfig implements configx.Configurable, reading the [aws] section into the
// AWS client's own config field.
func (a *AWS) ReadConfig(l configx.Reader) error {
	return l.Read("aws", &a.config)
}

// Init implements core.Initer, building the AWS SDK config from the loaded
// credentials and region, then initializing the requested service clients.
func (a *AWS) Init() error {
	var options []func(*awsConfig.LoadOptions) error
	if ak, sk := a.config.AccessKey, a.config.SecretKey; ak != "" && sk != "" {
		options = append(options, awsConfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(ak, sk, ""),
		))
	}
	if r := a.config.Region; r != "" {
		options = append(options, awsConfig.WithRegion(r))
	}
	// endpointURL redirects every client at one host — a local stack, or a
	// gateway standing in for AWS. Each client can still be pointed elsewhere by
	// its own endpoint setting below, which is applied after this one and wins.
	if ep := endpoint(a.config.EndpointURL); ep != "" {
		options = append(options, awsConfig.WithBaseEndpoint(ep))
	}
	a.DefaultS3BucketName = a.config.S3BucketName
	a.DefaultSQSQueueURL = a.config.SQSQueueURL
	a.DefaultSES = a.config.SES

	cfg, err := awsConfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return errorx.NewInternalError("aws", "load default config failed").WithInner(err)
	}
	a.DefaultConfig = awssdk.Config(cfg)
	return a.initClients()
}

// initClients builds the SDK clients named by a.services from a.DefaultConfig.
// Init and Derive share it so that adding a service — or changing how one of
// them is constructed — is a single edit rather than one per construction path.
func (a *AWS) initClients() error {
	for _, service := range a.services {
		switch service {
		case S3:
			a.S3Client = s3.NewFromConfig(a.DefaultConfig, func(o *s3.Options) {
				o.UsePathStyle = a.config.S3UsePathStyle
			})
		case SQS:
			a.SQSClient = sqs.NewFromConfig(a.DefaultConfig)
		case DynamoDB:
			var dynamoOpts []func(*dynamodb.Options)
			if ep := endpoint(a.config.DynamoDBEndpointURL); ep != "" {
				dynamoOpts = append(dynamoOpts, func(o *dynamodb.Options) {
					o.BaseEndpoint = awssdk.String(ep)
				})
			}
			a.DynamoDBClient = dynamodb.NewFromConfig(a.DefaultConfig, dynamoOpts...)
		case SES:
			var sesOpts []func(*sesv2.Options)
			if ep := strings.TrimSpace(a.config.SES.EndpointURL); ep != "" {
				sesOpts = append(sesOpts, func(o *sesv2.Options) {
					o.BaseEndpoint = awssdk.String(ep)
				})
			}
			a.SESClient = sesv2.NewFromConfig(a.DefaultConfig, sesOpts...)
		case STS:
			a.STSClient = sts.NewFromConfig(a.DefaultConfig)
		case SecretsManager:
			a.SecretsManagerClient = secretsmanager.NewFromConfig(a.DefaultConfig)
		default:
			return errorx.NewInternalError("aws", "undefined aws service", "service", service)
		}
	}
	return nil
}

// endpoint reads an optional endpoint override, returning "" when it is unset or
// blank so that a config key left empty never becomes a base URL no client can
// reach.
func endpoint(url *string) string {
	if url == nil {
		return ""
	}
	return strings.TrimSpace(*url)
}
