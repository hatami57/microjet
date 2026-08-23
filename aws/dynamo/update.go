package dynamo

import (
	"context"
	"maps"
	"reflect"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/hatami57/microjet/core/errorx"
)

// UpdateSpec describes a partial update: which attributes to write from the item,
// which to drop, and an optional condition the stored item must satisfy.
//
// Set and Remove hold DynamoDB attribute names (the names from the dynamodbav
// tags), not Go field names. auto_update fields are always added to Set.
// Removing an attribute is how a sparse index entry is cleared:
//
//	spec := dynamo.UpdateSpec{
//	    Set:       []string{"ReadAt"},
//	    Remove:    []string{"GSI1SK"},
//	    Condition: &unread,
//	}
type UpdateSpec struct {
	Set       []string                     // attribute names to SET from the item
	Remove    []string                     // attribute names to REMOVE
	Condition *expression.ConditionBuilder // optional ConditionExpression
}

// updateRequest is the marshalled form of an update, shared by UpdateWith and UpdateTx.
type updateRequest struct {
	key  map[string]dynamoTypes.AttributeValue
	expr expression.Expression
}

// UpdateWith applies the update described by spec to the item identified by item's
// pk/sk fields. It is the extended form of Update: same SET semantics, plus REMOVE
// and an optional ConditionExpression.
//
// When the condition does not hold the returned error matches ErrConditionFailed.
// When spec resolves to no attribute changes at all, UpdateWith returns nil without
// a round trip.
func (t *Table[T]) UpdateWith(ctx context.Context, item *T, spec UpdateSpec) error {
	req, err := t.buildUpdate(item, spec)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	_, err = t.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(t.tableName),
		Key:                       req.key,
		ExpressionAttributeNames:  req.expr.Names(),
		ExpressionAttributeValues: req.expr.Values(),
		UpdateExpression:          req.expr.Update(),
		ConditionExpression:       req.expr.Condition(),
	})
	if err != nil {
		return mapWriteError(err)
	}
	return nil
}

// buildUpdate applies auto_update timestamps to item and builds its key together
// with the SET/REMOVE/condition expression described by spec.
// It returns (nil, nil) when the spec yields no attribute changes.
func (t *Table[T]) buildUpdate(item *T, spec UpdateSpec) (*updateRequest, error) {
	// Merge caller-supplied field names with auto_update field names.
	setNames := make(map[string]struct{}, len(spec.Set)+len(t.meta.autoFields))
	for _, f := range spec.Set {
		setNames[f] = struct{}{}
	}
	for _, fm := range t.meta.autoFields {
		if fm.autoUpdate && fm.attrName != "" {
			setNames[fm.attrName] = struct{}{}
		}
	}

	removeNames := make(map[string]struct{}, len(spec.Remove))
	for _, name := range spec.Remove {
		// DynamoDB rejects an expression that touches the same attribute twice;
		// catch it here rather than paying a round trip to be told so.
		if _, ok := setNames[name]; ok {
			return nil, errorx.NewBadRequestError("dynamo",
				"attribute cannot be both SET and REMOVEd in one update", "attribute", name)
		}
		removeNames[name] = struct{}{}
	}

	// Only now that the spec is known to be valid: a rejected update must not
	// leave the caller's item carrying a new timestamp.
	applyTimestamps(reflect.ValueOf(item).Elem(), t.meta, true, t.clock.Now())

	key, err := t.buildKey(item)
	if err != nil {
		return nil, err
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}

	var update expression.UpdateBuilder
	clauses := 0
	for _, name := range slices.Sorted(maps.Keys(setNames)) {
		val, ok := av[name]
		if !ok {
			continue
		}
		update = update.Set(expression.Name(name), expression.Value(val))
		clauses++
	}
	for _, name := range slices.Sorted(maps.Keys(removeNames)) {
		update = update.Remove(expression.Name(name))
		clauses++
	}
	if clauses == 0 {
		return nil, nil
	}

	b := expression.NewBuilder().WithUpdate(update)
	if spec.Condition != nil {
		b = b.WithCondition(*spec.Condition)
	}
	expr, err := b.Build()
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}
	return &updateRequest{key: key, expr: expr}, nil
}
