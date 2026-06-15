package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hatami57/microjet/core/jsonx"
)

func (a *AWS) SQSSendMessage(ctx context.Context, message any) error {
	messageJSON, err := jsonx.ToJSON(message)
	if err != nil {
		return err
	}

	if a.SQSClient == nil {
		return fmt.Errorf("sqs client is not configured")
	}

	input := &sqs.SendMessageInput{
		MessageBody: &messageJSON,
		QueueUrl:    a.DefaultSQSQueueURL,
	}

	output, err := a.SQSClient.SendMessage(ctx, input)
	if err != nil {
		return fmt.Errorf("sqs send message: %w", err)
	}
	messageID := ""
	if output.MessageId != nil {
		messageID = *output.MessageId
	}
	a.Logger.Info("SQS send message successfully", "messageID", messageID)
	return nil
}
