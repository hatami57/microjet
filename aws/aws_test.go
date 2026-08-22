package aws

import (
	"log/slog"
	"testing"
)

func TestInitAppliesEndpointURLToEveryClient(t *testing.T) {
	const local = "http://localhost:4566"
	client := newTestAWS(Config{Region: "us-east-1", EndpointURL: new(local)}, S3, SQS, DynamoDB, SES)

	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if got := client.DefaultConfig.BaseEndpoint; got == nil || *got != local {
		t.Fatalf("shared base endpoint = %v, want %q", got, local)
	}
	// Every client inherits the shared endpoint through the SDK config, so a
	// single [aws] endpointURL is enough to point a service at a local stack.
	if got := client.S3Client.Options().BaseEndpoint; got == nil || *got != local {
		t.Errorf("s3 base endpoint = %v, want %q", got, local)
	}
	if got := client.SQSClient.Options().BaseEndpoint; got == nil || *got != local {
		t.Errorf("sqs base endpoint = %v, want %q", got, local)
	}
	if got := client.DynamoDBClient.Options().BaseEndpoint; got == nil || *got != local {
		t.Errorf("dynamodb base endpoint = %v, want %q", got, local)
	}
	if got := client.SESClient.Options().BaseEndpoint; got == nil || *got != local {
		t.Errorf("ses base endpoint = %v, want %q", got, local)
	}
}

func TestInitPerServiceEndpointWinsOverSharedOne(t *testing.T) {
	const (
		shared = "http://localhost:4566"
		dynamo = "http://localhost:8000"
		ses    = "http://localhost:9000"
	)
	client := newTestAWS(Config{
		Region:              "us-east-1",
		EndpointURL:         new(shared),
		DynamoDBEndpointURL: new(dynamo),
		SES:                 SESConfig{EndpointURL: ses},
	}, S3, DynamoDB, SES)

	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if got := client.DynamoDBClient.Options().BaseEndpoint; got == nil || *got != dynamo {
		t.Errorf("dynamodb base endpoint = %v, want its own %q", got, dynamo)
	}
	if got := client.SESClient.Options().BaseEndpoint; got == nil || *got != ses {
		t.Errorf("ses base endpoint = %v, want its own %q", got, ses)
	}
	if got := client.S3Client.Options().BaseEndpoint; got == nil || *got != shared {
		t.Errorf("s3 base endpoint = %v, want the shared %q", got, shared)
	}
}

func TestInitIgnoresBlankEndpoints(t *testing.T) {
	client := newTestAWS(Config{
		Region:              "us-east-1",
		EndpointURL:         new("   "),
		DynamoDBEndpointURL: new(""),
		SES:                 SESConfig{EndpointURL: " "},
	}, DynamoDB, SES)

	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// A key left blank in TOML must leave the SDK on its default endpoints rather
	// than send every request to an unreachable empty host.
	if got := client.DefaultConfig.BaseEndpoint; got != nil {
		t.Errorf("shared base endpoint = %q, want none", *got)
	}
	if got := client.DynamoDBClient.Options().BaseEndpoint; got != nil {
		t.Errorf("dynamodb base endpoint = %q, want none", *got)
	}
	if got := client.SESClient.Options().BaseEndpoint; got != nil {
		t.Errorf("ses base endpoint = %q, want none", *got)
	}
}

func TestInitS3PathStyle(t *testing.T) {
	client := newTestAWS(Config{Region: "us-east-1"}, S3)
	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if client.S3Client.Options().UsePathStyle {
		t.Error("path-style addressing must stay off unless asked for: AWS itself needs virtual-host URLs")
	}

	client = newTestAWS(Config{Region: "us-east-1", S3UsePathStyle: true}, S3)
	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !client.S3Client.Options().UsePathStyle {
		t.Error("s3UsePathStyle did not reach the client")
	}
}

func TestInitRejectsUnknownService(t *testing.T) {
	client := newTestAWS(Config{Region: "us-east-1"}, Service("aws-lambda"))

	if err := client.Init(); err == nil {
		t.Fatal("expected an error for a service the module does not know")
	}
}

// newTestAWS builds a client with config already in place, standing in for the
// host's config phase so Init can be exercised on its own.
func newTestAWS(config Config, services ...Service) *AWS {
	client := NewAWS(slog.New(slog.DiscardHandler), services...)
	client.config = config
	return client
}
