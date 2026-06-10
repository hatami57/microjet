package dynamo

import (
	"fmt"
	"time"

	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const milliTimeLayout = "2006-01-02T15:04:05.000Z07:00"

// Timestamp is a time.Time that marshals to/from DynamoDB as an ISO 8601 string
// with millisecond precision (e.g. "2026-06-03T06:30:01.715Z").
type Timestamp time.Time

func (t Timestamp) Time() time.Time {
	return time.Time(t)
}

func (t Timestamp) MarshalDynamoDBAttributeValue() (dynamoTypes.AttributeValue, error) {
	return &dynamoTypes.AttributeValueMemberS{
		Value: time.Time(t).UTC().Format(milliTimeLayout),
	}, nil
}

func (t *Timestamp) UnmarshalDynamoDBAttributeValue(av dynamoTypes.AttributeValue) error {
	s, ok := av.(*dynamoTypes.AttributeValueMemberS)
	if !ok {
		return fmt.Errorf("dynamo: cannot unmarshal %T into Timestamp", av)
	}
	parsed, err := time.Parse(milliTimeLayout, s.Value)
	if err != nil {
		// fall back to RFC3339Nano for records written before this change
		parsed, err = time.Parse(time.RFC3339Nano, s.Value)
		if err != nil {
			return fmt.Errorf("dynamo: parse timestamp %q: %w", s.Value, err)
		}
	}
	*t = Timestamp(parsed)
	return nil
}
