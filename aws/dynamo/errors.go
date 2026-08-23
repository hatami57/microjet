package dynamo

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/hatami57/microjet/core/errorx"
)

// conditionalCheckFailedCode is the CancellationReason code DynamoDB reports for a
// transaction item whose ConditionExpression did not hold.
const conditionalCheckFailedCode = "ConditionalCheckFailed"

// ErrConditionFailed reports that a ConditionExpression did not hold: the item was
// changed concurrently, was already processed, or did not match the expected state.
// It is a normal outcome of optimistic concurrency and idempotent writes, not an
// internal failure, and callers match it without inspecting message text:
//
//	if err := table.UpdateWith(ctx, item, spec); errors.Is(err, dynamo.ErrConditionFailed) {
//	    // somebody else got there first
//	}
var ErrConditionFailed = errorx.NewBusinessError("dynamo", "condition check failed")

// mapWriteError maps a failed write to an errorx error: a failed condition — on a
// single write or inside a transaction — becomes ErrConditionFailed, anything else
// stays internal.
func mapWriteError(err error) error {
	if _, ok := errors.AsType[*dynamoTypes.ConditionalCheckFailedException](err); ok {
		return ErrConditionFailed.WithInner(err)
	}
	if canceled, ok := errors.AsType[*dynamoTypes.TransactionCanceledException](err); ok {
		return mapCancellation(canceled)
	}
	return errorx.ErrInternal.WithInner(err)
}

// mapCancellation unpacks the per-item cancellation reasons of a cancelled
// transaction, so the caller learns which item failed and why instead of a bare
// "transaction cancelled".
func mapCancellation(canceled *dynamoTypes.TransactionCanceledException) error {
	reasons := make([]string, 0, len(canceled.CancellationReasons))
	conditionFailed := false
	for i, reason := range canceled.CancellationReasons {
		code := aws.ToString(reason.Code)
		// DynamoDB reports "None" for the items that were fine.
		if code == "" || code == "None" {
			continue
		}
		if code == conditionalCheckFailedCode {
			conditionFailed = true
		}
		if msg := aws.ToString(reason.Message); msg != "" {
			reasons = append(reasons, fmt.Sprintf("item %d: %s: %s", i, code, msg))
		} else {
			reasons = append(reasons, fmt.Sprintf("item %d: %s", i, code))
		}
	}
	if conditionFailed {
		return ErrConditionFailed.WithParams("reasons", reasons).WithInner(canceled)
	}
	return errorx.NewInternalError("dynamo", "transaction cancelled", "reasons", reasons).WithInner(canceled)
}
