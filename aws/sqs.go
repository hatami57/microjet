package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/jsonx"
)

func (a *AWS) SQSSendMessage(ctx context.Context, message any) error {
	messageJSON, err := jsonx.ToJSON(message)
	if err != nil {
		return err
	}

	if a.SQSClient == nil {
		return errorx.NewInternalError("aws", "sqs client is not configured")
	}

	input := &sqs.SendMessageInput{
		MessageBody: &messageJSON,
		QueueUrl:    a.DefaultSQSQueueURL,
	}

	output, err := a.SQSClient.SendMessage(ctx, input)
	if err != nil {
		return errorx.NewInternalError("aws", "sqs send message failed").WithInner(err)
	}
	messageID := ""
	if output.MessageId != nil {
		messageID = *output.MessageId
	}
	a.Logger.Info("SQS send message successfully", "messageID", messageID)
	return nil
}
