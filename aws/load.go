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

// LoadConfig loads the [aws] config section using core's shared viper setup, so
// the same config files and APP_AWS_* env overrides apply as for the rest of the
// app. Optional fields are left nil when neither config nor env provides them.
func LoadConfig(envPrefix string) (*Config, error) {
	return core.LoadSection[Config]("aws", envPrefix)
}