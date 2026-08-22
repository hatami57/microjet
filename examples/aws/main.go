// Command aws demonstrates MicroJet's AWS integration (aws.Module): unified
// initialization of S3, SQS, DynamoDB, and SES clients from the [aws] config
// section, reached with aws.Of(app).
//
// It wires the clients and shows the call sites for an S3 upload/download, an
// SQS send and an SES email. The actual network calls only run when you point
// [aws] at real infrastructure (AWS, or a local stack), so the program is safe
// to run offline:
//
//	go run .
//
// To exercise the calls, set a bucket, a queue and a sender in config.toml (and
// valid credentials/region or an endpointURL for a local stack such as
// LocalStack).
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
		WithModule(aws.Module(aws.S3, aws.SQS, aws.DynamoDB, aws.SES)).
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

	// SES: send an email. The sender defaults to [aws.ses].senderEmail, which has
	// to be a verified SES identity; SESSendEmail returns the provider message ID
	// that later delivery/bounce events carry.
	if client.DefaultSES.SenderEmail != "" {
		messageID, err := client.SESSendEmail(ctx, &aws.SESSendEmailRequest{
			To:       []string{"recipient@example.com"},
			Subject:  "Hello from microjet",
			HTMLBody: "<h1>Hello</h1><p>Sent through SES.</p>",
			TextBody: "Hello — sent through SES.",
		})
		switch {
		case err == nil:
			app.Logger.Info("ses send succeeded", "messageID", messageID)
		case aws.SESIsPermanentFailure(err):
			// Retrying would fail the same way: a sweeper would mark this one failed.
			app.Logger.Error("ses send rejected", "error", err)
		default:
			app.Logger.Error("ses send failed, retryable", "error", err)
		}
	} else {
		app.Logger.Info("SES skipped: set [aws.ses].senderEmail to send an email")
	}

	// DynamoDB: aws.Of(app).DynamoDBClient is a ready *dynamodb.Client you can use
	// with the AWS SDK directly.
	app.Logger.Info("AWS clients ready", "dynamodb", client.DynamoDBClient != nil)
}
