// Package aws provides initialized AWS service clients (S3, SQS, DynamoDB),
// wired into the host as a module that reads the [aws] config section.
package aws

import (
	"context"
	"log/slog"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
)

type AWS struct {
	DefaultConfig       awssdk.Config
	DefaultS3BucketName *string
	DefaultSQSQueueURL  *string
	Logger              *slog.Logger

	S3Client       *s3.Client
	SQSClient      *sqs.Client
	DynamoDBClient *dynamodb.Client

	config   Config
	services []Service
}

// NewAWS returns an AWS client ready to participate in the host service
// lifecycle. services lists which SDK clients (S3, SQS, DynamoDB) to
// initialize on Init.
func NewAWS(logger *slog.Logger, services ...Service) *AWS {
	return &AWS{Logger: logger, services: services}
}

type Service string

const (
	S3       Service = "aws-s3"
	SQS      Service = "aws-sqs"
	DynamoDB Service = "aws-dynamodb"
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
	a.DefaultS3BucketName = a.config.S3BucketName
	a.DefaultSQSQueueURL = a.config.SQSQueueURL

	cfg, err := awsConfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return errorx.NewInternalError("aws", "load default config failed").WithInner(err)
	}
	a.DefaultConfig = awssdk.Config(cfg)
	for _, service := range a.services {
		switch service {
		case S3:
			a.S3Client = s3.NewFromConfig(a.DefaultConfig)
		case SQS:
			a.SQSClient = sqs.NewFromConfig(a.DefaultConfig)
		case DynamoDB:
			var dynamoOpts []func(*dynamodb.Options)
			if a.config.DynamoDBEndpointURL != nil {
				ep := *a.config.DynamoDBEndpointURL
				dynamoOpts = append(dynamoOpts, func(o *dynamodb.Options) {
					o.BaseEndpoint = awssdk.String(ep)
				})
			}
			a.DynamoDBClient = dynamodb.NewFromConfig(a.DefaultConfig, dynamoOpts...)
		default:
			return errorx.NewInternalError("aws", "undefined aws service", "service", service)
		}
	}
	return nil
}
