package aws

// Config is MicroJet's AWS configuration, read from the [aws] section of the
// application config (with APP_AWS_* environment overrides). It lives in the aws
// module rather than in core so the core module stays vendor-neutral.
type Config struct {
	AccessKey string `mapstructure:"accessKey"`
	SecretKey string `mapstructure:"secretKey"`
	Region    string `mapstructure:"region"`
	// EndpointURL sends every client to one host instead of AWS — a local stack
	// such as LocalStack, or a gateway standing in for AWS. Per-service endpoints
	// (DynamoDBEndpointURL, SES.EndpointURL) override it for their own client.
	EndpointURL  *string `mapstructure:"endpointURL"`
	S3BucketName *string `mapstructure:"s3BucketName"`
	// S3UsePathStyle addresses buckets as endpoint/bucket/key instead of
	// bucket.endpoint/key. AWS itself needs the default (virtual-host) form; a
	// local S3 reached through EndpointURL — MinIO, LocalStack — usually needs
	// this one, because the per-bucket hostname it implies does not resolve.
	S3UsePathStyle bool    `mapstructure:"s3UsePathStyle"`
	SQSQueueURL    *string `mapstructure:"sqsQueueURL"`
	// DynamoDBEndpointURL points the DynamoDB client somewhere else than the rest,
	// typically at a local DynamoDB while the other clients talk to AWS.
	DynamoDBEndpointURL *string   `mapstructure:"dynamoDBEndpointURL"`
	SES                 SESConfig `mapstructure:"ses"`
}

// SESConfig is the [aws.ses] section: the sending defaults SESSendEmail applies
// when a request leaves them out.
type SESConfig struct {
	// SenderEmail is the default From address; it (or its domain) must be a
	// verified SES identity. SenderName is the display name shown next to it.
	SenderEmail string `mapstructure:"senderEmail"`
	SenderName  string `mapstructure:"senderName"`
	// ConfigurationSet names the configuration set applied to every send, which
	// is what publishes delivery/bounce/complaint events to SNS/EventBridge.
	ConfigurationSet string `mapstructure:"configurationSet"`
	// EndpointURL overrides the SES endpoint, for a local stack. It wins over the
	// [aws] endpointURL that applies to every other client.
	EndpointURL string `mapstructure:"endpointURL"`
}
