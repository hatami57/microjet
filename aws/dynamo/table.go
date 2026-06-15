// Package dynamo provides a generic, tag-driven DynamoDB table accessor.
//
// Define your DynamoDB item struct using the dynamo struct tag alongside the
// standard dynamodbav tag:
//
//	type UserItem struct {
//	    TenantID  uuid.UUID `dynamo:"pk,prefix=T:"        dynamodbav:"-"`
//	    ID        uuid.UUID `dynamo:"sk,prefix=U:"        dynamodbav:"-"`
//	    Email     string    `dynamodbav:"Email"`
//	    FirstName string    `dynamodbav:"FirstName"`
//	    CreatedAt time.Time `dynamo:"auto_create"         dynamodbav:"CreatedAt,unixtime"`
//	    UpdatedAt time.Time `dynamo:"auto_update"         dynamodbav:"UpdatedAt,unixtime"`
//	}
//
// Supported dynamo tag options:
//
//	pk              – this field is the partition key
//	sk              – this field is the sort key
//	prefix=X        – prepend X when encoding to DynamoDB; strip X on decode
//	const=X         – key component is always X; the field value is ignored on write
//	auto_create     – set to time.Now() on Put if the field is zero
//	auto_update     – set to time.Now() on Put and Update (always)
//
// PK/SK fields must also carry dynamodbav:"-" so the AWS marshaler ignores them;
// the Table handles encoding and decoding them directly via the dynamo tag.
package dynamo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/errorx"
)

// Table is a typed accessor for a single DynamoDB table.
// T must be a struct with at least one field tagged dynamo:"pk,..." and one tagged dynamo:"sk,...".
// Create one per item type during repository initialisation.
type Table[T any] struct {
	client    *dynamodb.Client
	tableName string
	meta      *structMeta
	clock     core.TimeProvider
}

// New creates a Table[T] and eagerly validates that T carries valid dynamo tags.
// Returns an error if the required pk/sk tags are missing.
func New[T any](client *dynamodb.Client, tableName string) (*Table[T], error) {
	meta, err := getStructMeta[T]()
	if err != nil {
		return nil, err
	}
	return &Table[T]{client: client, tableName: tableName, meta: meta, clock: core.UTC}, nil
}

// buildKey derives the DynamoDB {"PK": ..., "SK": ...} key map from item's tagged fields.
func (t *Table[T]) buildKey(item *T) (map[string]dynamoTypes.AttributeValue, error) {
	v := reflect.ValueOf(item).Elem()
	pkStr, err := t.meta.pkField.format.encode(v)
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}
	skStr, err := t.meta.skField.format.encode(v)
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}
	key, err := attributevalue.MarshalMap(map[string]string{"PK": pkStr, "SK": skStr})
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}
	return key, nil
}

// injectKeys reads the raw PK/SK attribute values from a DynamoDB response and decodes
// them back into the appropriate struct fields (stripping prefixes, parsing UUIDs, etc.).
func (t *Table[T]) injectKeys(item *T, raw map[string]dynamoTypes.AttributeValue) error {
	v := reflect.ValueOf(item).Elem()
	if pkAttr, ok := raw["PK"]; ok {
		var pkStr string
		if err := attributevalue.Unmarshal(pkAttr, &pkStr); err != nil {
			return errorx.ErrInternal.WithInner(err)
		}
		if err := t.meta.pkField.format.decode(pkStr, v); err != nil {
			return errorx.ErrInternal.WithInner(err)
		}
	}
	if skAttr, ok := raw["SK"]; ok {
		var skStr string
		if err := attributevalue.Unmarshal(skAttr, &skStr); err != nil {
			return errorx.ErrInternal.WithInner(err)
		}
		if err := t.meta.skField.format.decode(skStr, v); err != nil {
			return errorx.ErrInternal.WithInner(err)
		}
	}
	return nil
}

// unmarshalItem deserialises a raw DynamoDB attribute map into T and injects the decoded
// PK/SK values into the appropriate struct fields.
func (t *Table[T]) unmarshalItem(raw map[string]dynamoTypes.AttributeValue) (*T, error) {
	var item T
	if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}
	if err := t.injectKeys(&item, raw); err != nil {
		return nil, err
	}
	return &item, nil
}

// Put writes item to DynamoDB. auto_create fields are set to time.Now() when zero;
// auto_update fields are always overwritten with time.Now().
func (t *Table[T]) Put(ctx context.Context, item *T) error {
	applyTimestamps(reflect.ValueOf(item).Elem(), t.meta, false, t.clock.Now())

	key, err := t.buildKey(item)
	if err != nil {
		return err
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return errorx.ErrInternal.WithInner(err)
	}
	maps.Copy(av, key)
	_, err = t.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(t.tableName),
		Item:      av,
	})
	if err != nil {
		return errorx.ErrInternal.WithInner(err)
	}
	return nil
}

// Get fetches the item identified by the pk/sk-tagged fields in key.
// Returns (nil, false, nil) when the item does not exist.
func (t *Table[T]) Get(ctx context.Context, key *T) (*T, bool, error) {
	k, err := t.buildKey(key)
	if err != nil {
		return nil, false, err
	}
	res, err := t.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(t.tableName),
		Key:       k,
	})
	if err != nil {
		return nil, false, errorx.ErrInternal.WithInner(err)
	}
	if len(res.Item) == 0 {
		return nil, false, nil
	}
	item, err := t.unmarshalItem(res.Item)
	if err != nil {
		return nil, false, err
	}
	return item, true, nil
}

// Delete removes the item identified by the pk/sk-tagged fields in key.
func (t *Table[T]) Delete(ctx context.Context, key *T) error {
	k, err := t.buildKey(key)
	if err != nil {
		return err
	}
	_, err = t.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(t.tableName),
		Key:       k,
	})
	if err != nil {
		return errorx.ErrInternal.WithInner(err)
	}
	return nil
}

// Update applies a partial update to the item identified by item's pk/sk fields.
// Only the DynamoDB attribute names listed in fields are written; all auto_update fields
// are always included automatically. Fields are DynamoDB attribute names (from dynamodbav tags).
func (t *Table[T]) Update(ctx context.Context, item *T, fields ...string) error {
	applyTimestamps(reflect.ValueOf(item).Elem(), t.meta, true, t.clock.Now())

	key, err := t.buildKey(item)
	if err != nil {
		return err
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return errorx.ErrInternal.WithInner(err)
	}

	// Merge caller-supplied field names with auto_update field names.
	updateSet := make(map[string]struct{}, len(fields)+len(t.meta.autoFields))
	for _, f := range fields {
		updateSet[f] = struct{}{}
	}
	for _, fm := range t.meta.autoFields {
		if fm.autoUpdate && fm.attrName != "" {
			updateSet[fm.attrName] = struct{}{}
		}
	}

	var update expression.UpdateBuilder
	first := true
	for name := range updateSet {
		val, ok := av[name]
		if !ok {
			continue
		}
		if first {
			update = expression.Set(expression.Name(name), expression.Value(val))
			first = false
		} else {
			update = update.Set(expression.Name(name), expression.Value(val))
		}
	}
	if first {
		return nil
	}

	expr, err := expression.NewBuilder().WithUpdate(update).Build()
	if err != nil {
		return errorx.ErrInternal.WithInner(err)
	}
	_, err = t.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(t.tableName),
		Key:                       key,
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		UpdateExpression:          expr.Update(),
	})
	if err != nil {
		return errorx.ErrInternal.WithInner(err)
	}
	return nil
}

// BatchGet fetches all items identified by keys, automatically batching into 100-item
// chunks and retrying any UnprocessedKeys returned by DynamoDB.
func (t *Table[T]) BatchGet(ctx context.Context, keys []*T) ([]*T, error) {
	const batchSize = 100
	var result []*T

	for i := 0; i < len(keys); i += batchSize {
		end := min(i+batchSize, len(keys))
		rawKeys := make([]map[string]dynamoTypes.AttributeValue, end-i)
		for j, k := range keys[i:end] {
			rawKey, err := t.buildKey(k)
			if err != nil {
				return nil, err
			}
			rawKeys[j] = rawKey
		}

		requestItems := map[string]dynamoTypes.KeysAndAttributes{
			t.tableName: {Keys: rawKeys},
		}
		for len(requestItems) > 0 {
			res, err := t.client.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{
				RequestItems: requestItems,
			})
			if err != nil {
				return nil, errorx.ErrInternal.WithInner(err)
			}
			for _, items := range res.Responses {
				for _, raw := range items {
					item, err := t.unmarshalItem(raw)
					if err != nil {
						return nil, err
					}
					result = append(result, item)
				}
			}
			requestItems = res.UnprocessedKeys
		}
	}
	return result, nil
}

// QueryGSIPage fetches one page of items from a GSI whose partition key attribute equals
// pkValue. An optional skCond narrows results by the GSI sort key; pass nil to match all
// items under the partition key. Pagination tokens are automatically decoded on input and
// encoded on output.
func (t *Table[T]) QueryGSIPage(ctx context.Context, indexName, pkAttr, pkValue string, skCond *SKCondition, pageSize int32, token *string) ([]*T, *string, error) {
	var startKey map[string]dynamoTypes.AttributeValue
	if token != nil {
		decoded, err := base64.StdEncoding.DecodeString(*token)
		if err != nil {
			return nil, nil, errorx.ErrBadRequest.WithSubject("NextPageToken").WithInner(err)
		}
		if err = json.Unmarshal(decoded, &startKey); err != nil {
			return nil, nil, errorx.ErrBadRequest.WithSubject("NextPageToken").WithInner(err)
		}
	}

	keyEx := expression.Key(pkAttr).Equal(expression.Value(pkValue))
	if skCond != nil {
		keyEx = skCond.apply(keyEx)
	}
	expr, err := expression.NewBuilder().WithKeyCondition(keyEx).Build()
	if err != nil {
		return nil, nil, errorx.ErrInternal.WithInner(err)
	}

	paginator := dynamodb.NewQueryPaginator(t.client, &dynamodb.QueryInput{
		TableName:                 aws.String(t.tableName),
		IndexName:                 aws.String(indexName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
		ExclusiveStartKey:         startKey,
		Limit:                     aws.Int32(pageSize),
	})
	output, err := paginator.NextPage(ctx)
	if err != nil {
		return nil, nil, errorx.ErrInternal.WithInner(err)
	}

	items := make([]*T, 0, len(output.Items))
	for _, raw := range output.Items {
		item, err := t.unmarshalItem(raw)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}

	var nextToken *string
	if output.LastEvaluatedKey != nil {
		encoded, err := json.Marshal(output.LastEvaluatedKey)
		if err != nil {
			return nil, nil, errorx.ErrInternal.WithInner(err)
		}
		s := base64.StdEncoding.EncodeToString(encoded)
		nextToken = &s
	}
	return items, nextToken, nil
}

// QueryPage fetches one page of items whose PK matches the pk-tagged field of pkItem,
// optionally filtered by an SK prefix. Pagination tokens are automatically decoded on
// input and encoded on output.
func (t *Table[T]) QueryPage(ctx context.Context, pkItem *T, skPrefix string, pageSize int32, token *string) ([]*T, *string, error) {
	pkStr, err := t.meta.pkField.format.encode(reflect.ValueOf(pkItem).Elem())
	if err != nil {
		return nil, nil, errorx.ErrInternal.WithInner(err)
	}

	var startKey map[string]dynamoTypes.AttributeValue
	if token != nil {
		decoded, err := base64.StdEncoding.DecodeString(*token)
		if err != nil {
			return nil, nil, errorx.ErrBadRequest.WithSubject("NextPageToken").WithInner(err)
		}
		if err = json.Unmarshal(decoded, &startKey); err != nil {
			return nil, nil, errorx.ErrBadRequest.WithSubject("NextPageToken").WithInner(err)
		}
	}

	keyEx := expression.Key("PK").Equal(expression.Value(pkStr))
	if skPrefix != "" {
		keyEx = keyEx.And(expression.Key("SK").BeginsWith(skPrefix))
	}
	expr, err := expression.NewBuilder().WithKeyCondition(keyEx).Build()
	if err != nil {
		return nil, nil, errorx.ErrInternal.WithInner(err)
	}

	paginator := dynamodb.NewQueryPaginator(t.client, &dynamodb.QueryInput{
		TableName:                 aws.String(t.tableName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
		ExclusiveStartKey:         startKey,
		Limit:                     aws.Int32(pageSize),
	})
	output, err := paginator.NextPage(ctx)
	if err != nil {
		return nil, nil, errorx.ErrInternal.WithInner(err)
	}

	items := make([]*T, 0, len(output.Items))
	for _, raw := range output.Items {
		item, err := t.unmarshalItem(raw)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}

	var nextToken *string
	if output.LastEvaluatedKey != nil {
		encoded, err := json.Marshal(output.LastEvaluatedKey)
		if err != nil {
			return nil, nil, errorx.ErrInternal.WithInner(err)
		}
		s := base64.StdEncoding.EncodeToString(encoded)
		nextToken = &s
	}
	return items, nextToken, nil
}
