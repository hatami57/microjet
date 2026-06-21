// Command aws demonstrates MicroJet's AWS integration (aws.Module): unified
// initialization of S3, SQS, and DynamoDB clients from the [aws] config section,
// reached with aws.Of(app).
//
// It wires the clients and shows the call sites for an S3 upload/download and an
// SQS send. The actual network calls only run when you point [aws] at real
// infrastructure (AWS, or a local stack), so the program is safe to run offline:
//
//	go run .
//
// To exercise the calls, set a bucket and queue in config.toml (and valid
// credentials/region or an endpointURL for a local stack such as LocalStack).
package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/hatami57/microjet/aws"
	"github.com/hatami57/microjet/host"
)

func main() {
	// Initialize the requested service clients without starting any server:
	// InitServices runs config + connect and returns. Add or drop services here
	// to control which SDK clients are built.
	app := host.MustNew().
		WithModule(aws.Module(aws.S3, aws.SQS, aws.DynamoDB)).
		InitServices()
	if err := app.Err(); err != nil {
		panic(err)
	}
	defer app.Close()

	ctx := context.Background()
	client := aws.Of(app)

	// S3: upload a local file, then download it back. Guarded by a configured
	// bucket so the example is a no-op until you set [aws].s3BucketName.
	if client.DefaultS3BucketName != nil {
		tmp := filepath.Join(os.TempDir(), "microjet-aws-demo.txt")
		_ = os.WriteFile(tmp, []byte("hello from microjet"), 0o600)

		if err := client.S3UploadFile(ctx, &aws.S3UploadFileRequest{
			BucketName:    *client.DefaultS3BucketName,
			ObjectKey:     "demo/hello.txt",
			ContentType:   "text/plain",
			LocalFilePath: tmp,
		}); err != nil {
			app.Logger.Error("s3 upload failed", "error", err)
		}

		if err := client.S3DownloadFile(ctx, &aws.S3DownloadFileRequest{
			BucketName:    *client.DefaultS3BucketName,
			ObjectKey:     "demo/hello.txt",
			LocalFilePath: tmp + ".downloaded",
		}); err != nil {
			app.Logger.Error("s3 download failed", "error", err)
		}
	} else {
		app.Logger.Info("S3 skipped: set [aws].s3BucketName to run upload/download")
	}

	// SQS: send a JSON message to the configured queue. SQSSendMessage marshals
	// any value to JSON for you.
	if client.DefaultSQSQueueURL != nil {
		if err := client.SQSSendMessage(ctx, map[string]any{"event": "user.created", "id": 42}); err != nil {
			app.Logger.Error("sqs send failed", "error", err)
		}
	} else {
		app.Logger.Info("SQS skipped: set [aws].sqsQueueURL to send a message")
	}

	// DynamoDB: aws.Of(app).DynamoDBClient is a ready *dynamodb.Client you can use
	// with the AWS SDK directly.
	app.Logger.Info("AWS clients ready", "dynamodb", client.DynamoDBClient != nil)
}
