package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/jsonx"
)

// SQSSendMessage sends message, marshaled to JSON, to the queue configured as
// [aws] sqsQueueURL. Use SQSSendMessageTo to target a different queue.
func (a *AWS) SQSSendMessage(ctx context.Context, message any) error {
	if a.DefaultSQSQueueURL == nil || *a.DefaultSQSQueueURL == "" {
		return errorx.NewInternalError("aws", "default sqs queue url is not configured")
	}

	return a.SQSSendMessageTo(ctx, *a.DefaultSQSQueueURL, message)
}

// SQSSendMessageTo sends message, marshaled to JSON, to queueURL, for callers
// that publish to a queue other than the configured default.
func (a *AWS) SQSSendMessageTo(ctx context.Context, queueURL string, message any) error {
	if queueURL == "" {
		return errorx.NewInternalError("aws", "sqs queue url is empty")
	}

	messageJSON, err := jsonx.ToJSON(message)
	if err != nil {
		return err
	}

	if a.SQSClient == nil {
		return errorx.NewInternalError("aws", "sqs client is not configured")
	}

	input := &sqs.SendMessageInput{
		MessageBody: &messageJSON,
		QueueUrl:    &queueURL,
	}

	output, err := a.SQSClient.SendMessage(ctx, input)
	if err != nil {
		return errorx.NewInternalError("aws", "sqs send message failed").WithInner(err)
	}
	messageID := ""
	if output.MessageId != nil {
		messageID = *output.MessageId
	}
	a.Logger.Info("SQS send message successfully", "messageID", messageID, "queueURL", queueURL)
	return nil
}
