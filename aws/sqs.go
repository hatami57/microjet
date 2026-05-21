package aws

import (
	"context"

	"github.com/hatami57/microjet/utils"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func (a *AWS) SQSSendMessage(ctx context.Context, message any) error {
	messageJSON, err := utils.ToJSON(message)
	if err != nil {
		return err
	}

	input := &sqs.SendMessageInput{
		MessageBody: &messageJSON,
		QueueUrl:    a.DefaultSQSQueueURL,
	}

	if a.SQSClient != nil {
		output, err := a.SQSClient.SendMessage(ctx, input)
		if err != nil {
			return err
		}
		a.Logger.Info("SQS send message successfully", "messageID", *output.MessageId)
	} else {
		a.Logger.Error("SQS client is not configured")
	}

	return nil
}
