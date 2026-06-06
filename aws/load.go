package aws

import (
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type AWS struct {
	DefaultConfig       aws.Config
	DefaultS3BucketName *string
	DefaultSQSQueueURL  *string
	Logger              *slog.Logger

	S3Client       *s3.Client
	SQSClient      *sqs.Client
	DynamoDBClient *dynamodb.Client

	config   Config
	services []AWSService
}

// NewAWS returns an AWS client ready to participate in the host service
// lifecycle. services lists which SDK clients (S3, SQS, DynamoDB) to
// initialize on Init.
func NewAWS(logger *slog.Logger, services ...AWSService) *AWS {
	return &AWS{Logger: logger, services: services}
}

type AWSService string

const (
	S3       AWSService = "aws-s3"
	SQS      AWSService = "aws-sqs"
	DynamoDB AWSService = "aws-dynamodb"
)

func (a *AWS) InitServices(services ...AWSService) error {
	for _, service := range services {
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
