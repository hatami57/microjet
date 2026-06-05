package aws

import "github.com/hatami57/microjet/core"

// Config is microjet's AWS configuration, read from the [aws] section of the
// application config (with APP_AWS_* environment overrides). It lives in the aws
// module rather than in core so the core module stays vendor-neutral.
type Config struct {
	AccessKey           string
	SecretKey           string
	Region              string
	EndpointURL         *string
	S3BucketName        *string
	SQSQueueURL         *string
	DynamoDBEndpointURL *string
}

// LoadConfig loads the [aws] config section using core's shared viper setup, so
// the same config files and APP_AWS_* env overrides apply as for the rest of the
// app. Optional fields are left nil when neither config nor env provides them.
func LoadConfig(envPrefix string) (*Config, error) {
	v, err := core.ConfigViper(envPrefix)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		AccessKey: v.GetString("aws.accessKey"),
		SecretKey: v.GetString("aws.secretKey"),
		Region:    v.GetString("aws.region"),
	}
	optStr := func(key string) *string {
		if v.IsSet(key) {
			s := v.GetString(key)
			return &s
		}
		return nil
	}
	cfg.EndpointURL = optStr("aws.endpointURL")
	cfg.S3BucketName = optStr("aws.s3BucketName")
	cfg.SQSQueueURL = optStr("aws.sqsQueueURL")
	cfg.DynamoDBEndpointURL = optStr("aws.dynamoDBEndpointURL")
	return cfg, nil
}
