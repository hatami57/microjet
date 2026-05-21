package host

import (
	"context"
	"os"

	"github.com/hatami57/microjet/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

func (a *App) WithAWS(services ...aws.AWSService) *App {
	a.AWS = &aws.AWS{Logger: a.Logger}

	var options []func(*awsConfig.LoadOptions) error

	if a.Config.AWS != nil {
		if ak, sk := a.Config.AWS.AccessKey, a.Config.AWS.SecretKey; ak != "" && sk != "" {
			options = append(options, awsConfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(ak, sk, ""),
			))
		}
		if r := a.Config.AWS.Region; r != "" {
			options = append(options, awsConfig.WithRegion(r))
		}
		a.AWS.DefaultS3BucketName = a.Config.AWS.S3BucketName
		a.AWS.DefaultSQSQueueURL = a.Config.AWS.SQSQueueURL
	}

	cfg, err := awsConfig.LoadDefaultConfig(context.TODO(), options...)
	if err != nil {
		a.Close()
		a.Logger.Error("failed to load aws config", "error", err)
		os.Exit(1)
	}
	a.AWS.DefaultConfig = cfg

	if err = a.AWS.InitServices(services...); err != nil {
		a.Close()
		a.Logger.Error("failed to init aws services", "error", err)
		os.Exit(1)
	}

	return a
}
