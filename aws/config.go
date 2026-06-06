package aws

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hatami57/microjet/core"
)

// Config is microjet's AWS configuration, read from the [aws] section of the
// application config (with APP_AWS_* environment overrides). It lives in the aws
// module rather than in core so the core module stays vendor-neutral.
type Config struct {
	AccessKey           string  `mapstructure:"accessKey"`
	SecretKey           string  `mapstructure:"secretKey"`
	Region              string  `mapstructure:"region"`
	EndpointURL         *string `mapstructure:"endpointURL"`
	S3BucketName        *string `mapstructure:"s3BucketName"`
	SQSQueueURL         *string `mapstructure:"sqsQueueURL"`
	DynamoDBEndpointURL *string `mapstructure:"dynamoDBEndpointURL"`
}

// LoadConfig implements core.Configurable, reading the [aws] section into the
// AWS client's own config field.
func (a *AWS) LoadConfig(l *core.ConfigLoader) error {
	return l.UnmarshalKey("aws", &a.config)
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
		return fmt.Errorf("aws: load default config: %w", err)
	}
	a.DefaultConfig = awssdk.Config(cfg)
	for _, service := range a.services {
		switch service {
		case S3:
			a.S3Client = s3.NewFromConfig(a.DefaultConfig)
		case SQS:
			a.SQSClient = sqs.NewFromConfig(a.DefaultConfig)
		case DynamoDB:
			a.DynamoDBClient = dynamodb.NewFromConfig(a.DefaultConfig)
		default:
			return fmt.Errorf("undefined aws service: %s", service)
		}
	}
	return nil
}
