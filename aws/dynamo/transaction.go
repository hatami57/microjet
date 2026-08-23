package dynamo

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/hatami57/microjet/core/errorx"
)

// maxTransactItems is DynamoDB's per-transaction item limit.
const maxTransactItems = 100

// PutTx builds a transactional Put for item, optionally guarded by cond
// (pass nil for an unconditional write). Keys, timestamps and marshalling are
// identical to Put.
//
//	notExists := expression.AttributeNotExists(expression.Name("PK"))
//	write, err := table.PutTx(item, &notExists)
func (t *Table[T]) PutTx(item *T, cond *expression.ConditionBuilder) (dynamoTypes.TransactWriteItem, error) {
	av, err := t.marshalItem(item)
	if err != nil {
		return dynamoTypes.TransactWriteItem{}, err
	}
	put := &dynamoTypes.Put{
		TableName: aws.String(t.tableName),
		Item:      av,
	}
	if cond != nil {
		expr, err := buildCondition(*cond)
		if err != nil {
			return dynamoTypes.TransactWriteItem{}, err
		}
		put.ConditionExpression = expr.Condition()
		put.ExpressionAttributeNames = expr.Names()
		put.ExpressionAttributeValues = expr.Values()
	}
	return dynamoTypes.TransactWriteItem{Put: put}, nil
}

// UpdateTx builds a transactional Update from spec, applying the same SET/REMOVE,
// timestamp and condition rules as UpdateWith.
// A spec that yields no attribute changes is an error: a transaction item cannot
// be empty.
func (t *Table[T]) UpdateTx(item *T, spec UpdateSpec) (dynamoTypes.TransactWriteItem, error) {
	req, err := t.buildUpdate(item, spec)
	if err != nil {
		return dynamoTypes.TransactWriteItem{}, err
	}
	if req == nil {
		return dynamoTypes.TransactWriteItem{}, errorx.NewBadRequestError("dynamo",
			"update transaction item has no SET or REMOVE clause")
	}
	return dynamoTypes.TransactWriteItem{Update: &dynamoTypes.Update{
		TableName:                 aws.String(t.tableName),
		Key:                       req.key,
		UpdateExpression:          req.expr.Update(),
		ConditionExpression:       req.expr.Condition(),
		ExpressionAttributeNames:  req.expr.Names(),
		ExpressionAttributeValues: req.expr.Values(),
	}}, nil
}

// DeleteTx builds a transactional Delete for the item identified by key's pk/sk
// fields, optionally guarded by cond (pass nil for an unconditional delete).
func (t *Table[T]) DeleteTx(key *T, cond *expression.ConditionBuilder) (dynamoTypes.TransactWriteItem, error) {
	k, err := t.buildKey(key)
	if err != nil {
		return dynamoTypes.TransactWriteItem{}, err
	}
	del := &dynamoTypes.Delete{
		TableName: aws.String(t.tableName),
		Key:       k,
	}
	if cond != nil {
		expr, err := buildCondition(*cond)
		if err != nil {
			return dynamoTypes.TransactWriteItem{}, err
		}
		del.ConditionExpression = expr.Condition()
		del.ExpressionAttributeNames = expr.Names()
		del.ExpressionAttributeValues = expr.Values()
	}
	return dynamoTypes.TransactWriteItem{Delete: del}, nil
}

// ConditionCheckTx builds a transactional check: the item identified by key's
// pk/sk fields must satisfy cond for the transaction to commit, but is not itself
// written. Use it to make a write depend on an item of a different type.
func (t *Table[T]) ConditionCheckTx(key *T, cond expression.ConditionBuilder) (dynamoTypes.TransactWriteItem, error) {
	k, err := t.buildKey(key)
	if err != nil {
		return dynamoTypes.TransactWriteItem{}, err
	}
	expr, err := buildCondition(cond)
	if err != nil {
		return dynamoTypes.TransactWriteItem{}, err
	}
	return dynamoTypes.TransactWriteItem{ConditionCheck: &dynamoTypes.ConditionCheck{
		TableName:                 aws.String(t.tableName),
		Key:                       k,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}}, nil
}

// TransactWrite commits items as a single all-or-nothing write. The items come
// from the *Tx builders and may span several tables and item types:
//
//	write, err := users.PutTx(user, &notExists)
//	claim, err := counters.UpdateTx(counter, spec)
//	err = dynamo.TransactWrite(ctx, client, write, claim)
//
// Passing no items is a no-op. More than 100 items is rejected before the request
// goes out, as is a batch DynamoDB cancels: when a condition inside the
// transaction fails the returned error matches ErrConditionFailed, and either way
// the error names the item index that failed and why.
func TransactWrite(ctx context.Context, client *dynamodb.Client, items ...dynamoTypes.TransactWriteItem) error {
	if len(items) == 0 {
		return nil
	}
	if len(items) > maxTransactItems {
		return errorx.NewBadRequestError("dynamo",
			"transaction exceeds the DynamoDB item limit",
			"items", len(items), "limit", maxTransactItems)
	}
	_, err := client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: items,
	})
	if err != nil {
		return mapWriteError(err)
	}
	return nil
}

// buildCondition builds a standalone ConditionExpression.
func buildCondition(cond expression.ConditionBuilder) (expression.Expression, error) {
	expr, err := expression.NewBuilder().WithCondition(cond).Build()
	if err != nil {
		return expression.Expression{}, errorx.ErrInternal.WithInner(err)
	}
	return expr, nil
}
