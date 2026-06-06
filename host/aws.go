package host

import (
	"context"
	"fmt"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/hatami57/microjet/aws"
)

// WithAWS loads AWS configuration and initializes the requested service clients
// (S3, SQS, DynamoDB). Errors are deferred to Run/MustRun/Err.
func (a *App) WithAWS(services ...aws.AWSService) *App {
	if a.err != nil {
		return a
	}
	a.AWS = &aws.AWS{Logger: a.Logger}

	awsCfg := &aws.Config{}
	if err := a.configLoader.Configure(awsCfg); err != nil {
		return a.fail(fmt.Errorf("aws: load config: %w", err))
	}

	var options []func(*awsConfig.LoadOptions) error
	if ak, sk := awsCfg.AccessKey, awsCfg.SecretKey; ak != "" && sk != "" {
		options = append(options, awsConfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(ak, sk, ""),
		))
	}
	if r := awsCfg.Region; r != "" {
		options = append(options, awsConfig.WithRegion(r))
	}
	a.AWS.DefaultS3BucketName = awsCfg.S3BucketName
	a.AWS.DefaultSQSQueueURL = awsCfg.SQSQueueURL

	cfg, err := awsConfig.LoadDefaultConfig(context.TODO(), options...)
	if err != nil {
		return a.fail(fmt.Errorf("aws: aws-sdk load config: %w", err))
	}
	a.AWS.DefaultConfig = cfg

	if err = a.AWS.InitServices(services...); err != nil {
		return a.fail(fmt.Errorf("aws: init services: %w", err))
	}

	return a
}
