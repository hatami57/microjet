package aws

import "github.com/hatami57/microjet/core"

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

// LoadConfig implements core.Configurable, loading the [aws] section.
func (c *Config) LoadConfig(l *core.ConfigLoader) error {
	return l.UnmarshalKey("aws", c)
}
