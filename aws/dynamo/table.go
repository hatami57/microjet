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
//	format=P        – build the value from pattern P (see below)
//	prefix=X        – prepend X when encoding to DynamoDB; strip X on decode (pk/sk only)
//	const=X         – value is always the literal X; the field's own value is ignored
//	auto_create     – set to time.Now() on Put if the field is zero
//	auto_update     – set to time.Now() on Put and Update (always)
//
// An option the package does not recognise is rejected by New rather than ignored,
// so a typo such as format:X cannot silently fall back to a bare key.
//
// PK/SK fields must also carry dynamodbav:"-" so the AWS marshaler ignores them;
// the Table handles encoding and decoding them directly via the dynamo tag.
//
// # Composite keys with format=
//
// format=P builds a value from a pattern of literals and {FieldName} placeholders,
// and is the only way to compose a key from more than one struct field:
//
//	TenantID uuid.UUID `dynamo:"pk,format=T:{TenantID}#U:{UserID}" dynamodbav:"-"`
//
// {FieldName} may reference any field of the struct, not just the tagged one, and
// the bare {} refers to the tagged field's own value. prefix= and const= are sugar
// over it: prefix=X means format=X{}, const=X means format=X (a pure literal that
// ignores the field's value entirely).
//
// Every field a key pattern references is encoded on write and decoded back on read,
// so it must be of a supported key type: string, uuid.UUID, or a type implementing
// encoding.TextMarshaler and encoding.TextUnmarshaler (ulid.ULID, netip.Addr,
// time.Time, and user types that implement the pair). New rejects anything else.
//
// Decoding splits the stored key on the literal segments, so two placeholders with
// no literal between them ("{TenantID}{UserID}") cannot be decoded — always keep a
// separator between adjacent placeholders.
//
// # Derived attributes
//
// const= and format= also work on ordinary, non-key fields, which is how a type
// discriminator or a GSI key stops being something every caller has to remember to
// fill in:
//
//	Type   string `dynamo:"const=MESSAGE"           dynamodbav:"Type"`
//	GSI1PK string `dynamo:"format=T:{TenantID}#MSG" dynamodbav:"GSI1_PK,omitempty"`
//	GSI1SK string `dynamo:"format=M:{ID}"           dynamodbav:"GSI1_SK,omitempty"`
//
// Such a field is recomputed from its source fields on every Put and Update, so it
// cannot drift from the data it is built out of. Unlike a key, it is stored and read
// back as a normal attribute — it is never decoded, so its source fields only need to
// implement encoding.TextMarshaler, not the unmarshaling half.
//
// A derived field must be a string, must be persisted (not dynamodbav:"-"), and must
// not reference itself: a pattern that read its own value would re-encode what the
// previous write produced and grow the value every time. That rules out prefix= and
// the bare {} on a non-key field; both are rejected by New.
//
// Update writes a derived attribute only when the attribute is named in the update —
// recomputing it does not by itself add it to the UpdateExpression:
//
//	table.Update(ctx, msg, "Title", "GSI1_SK")
package dynamo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

// marshalItem applies the write timestamps and derived attributes to item and marshals
// it into a full attribute map, with the encoded PK/SK attributes merged in.
// Shared by Put and PutTx.
func (t *Table[T]) marshalItem(item *T) (map[string]dynamoTypes.AttributeValue, error) {
	v := reflect.ValueOf(item).Elem()
	applyTimestamps(v, t.meta, false, t.clock.Now())
	if err := applyDerived(v, t.meta); err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}

	key, err := t.buildKey(item)
	if err != nil {
		return nil, err
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}
	maps.Copy(av, key)
	return av, nil
}

// Put writes item to DynamoDB. auto_create fields are set to time.Now() when zero;
// auto_update fields are always overwritten with time.Now().
func (t *Table[T]) Put(ctx context.Context, item *T) error {
	av, err := t.marshalItem(item)
	if err != nil {
		return err
	}
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
//
// Update is the SET-only form of UpdateWith; use UpdateWith to also REMOVE
// attributes or to guard the write with a condition.
func (t *Table[T]) Update(ctx context.Context, item *T, fields ...string) error {
	return t.UpdateWith(ctx, item, UpdateSpec{Set: fields})
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

// pageKeyAttr is the JSON form of one key attribute inside a pagination token.
// AttributeValue is an interface, which encoding/json cannot decode into, so the
// token stores the DynamoDB wire shape instead — a key attribute is always a
// string, a number or binary.
type pageKeyAttr struct {
	S *string `json:"S,omitempty"`
	N *string `json:"N,omitempty"`
	B []byte  `json:"B,omitempty"`
}

// decodePageToken decodes a base64 pagination token into an ExclusiveStartKey.
func decodePageToken(token *string) (map[string]dynamoTypes.AttributeValue, error) {
	if token == nil {
		return nil, nil
	}
	badToken := func(err error) error {
		return errorx.ErrBadRequest.WithSubject("NextPageToken").WithInner(err)
	}

	decoded, err := base64.StdEncoding.DecodeString(*token)
	if err != nil {
		return nil, badToken(err)
	}
	var raw map[string]pageKeyAttr
	if err = json.Unmarshal(decoded, &raw); err != nil {
		return nil, badToken(err)
	}

	startKey := make(map[string]dynamoTypes.AttributeValue, len(raw))
	for name, attr := range raw {
		switch {
		case attr.S != nil:
			startKey[name] = &dynamoTypes.AttributeValueMemberS{Value: *attr.S}
		case attr.N != nil:
			startKey[name] = &dynamoTypes.AttributeValueMemberN{Value: *attr.N}
		case attr.B != nil:
			startKey[name] = &dynamoTypes.AttributeValueMemberB{Value: attr.B}
		default:
			return nil, badToken(errorx.NewBadRequestError("dynamo", "empty key attribute", "attribute", name))
		}
	}
	return startKey, nil
}

// encodePageToken encodes a LastEvaluatedKey into a base64 pagination token,
// returning nil when the last page has been reached.
func encodePageToken(lastKey map[string]dynamoTypes.AttributeValue) (*string, error) {
	if lastKey == nil {
		return nil, nil
	}

	raw := make(map[string]pageKeyAttr, len(lastKey))
	for name, attr := range lastKey {
		switch v := attr.(type) {
		case *dynamoTypes.AttributeValueMemberS:
			raw[name] = pageKeyAttr{S: &v.Value}
		case *dynamoTypes.AttributeValueMemberN:
			raw[name] = pageKeyAttr{N: &v.Value}
		case *dynamoTypes.AttributeValueMemberB:
			raw[name] = pageKeyAttr{B: v.Value}
		default:
			return nil, errorx.NewInternalError("dynamo", "unsupported key attribute type",
				"attribute", name, "type", fmt.Sprintf("%T", attr))
		}
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}
	s := base64.StdEncoding.EncodeToString(encoded)
	return &s, nil
}

// tableQueryInput builds the QueryInput shared by QueryPage and Count.
func (t *Table[T]) tableQueryInput(pkItem *T, skPrefix string, opts *queryOptions) (*dynamodb.QueryInput, error) {
	pkStr, err := t.meta.pkField.format.encode(reflect.ValueOf(pkItem).Elem())
	if err != nil {
		return nil, errorx.ErrInternal.WithInner(err)
	}

	keyEx := expression.Key("PK").Equal(expression.Value(pkStr))
	if skPrefix != "" {
		keyEx = keyEx.And(expression.Key("SK").BeginsWith(skPrefix))
	}
	expr, err := opts.buildExpression(keyEx)
	if err != nil {
		return nil, err
	}

	in := &dynamodb.QueryInput{
		TableName:                 aws.String(t.tableName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
	}
	opts.applyTo(in)
	return in, nil
}

// gsiQueryInput builds the QueryInput shared by QueryGSIPage and CountGSI.
func (t *Table[T]) gsiQueryInput(indexName, pkAttr, pkValue string, skCond *SKCondition, opts *queryOptions) (*dynamodb.QueryInput, error) {
	if opts.consistent {
		// DynamoDB only serves eventually consistent reads from a GSI; fail here
		// rather than letting the request be rejected at the far end.
		return nil, errorx.NewInternalError("dynamo",
			"ConsistentRead is not supported on a global secondary index", "index", indexName)
	}

	keyEx := expression.Key(pkAttr).Equal(expression.Value(pkValue))
	if skCond != nil {
		keyEx = skCond.apply(keyEx)
	}
	expr, err := opts.buildExpression(keyEx)
	if err != nil {
		return nil, err
	}

	in := &dynamodb.QueryInput{
		TableName:                 aws.String(t.tableName),
		IndexName:                 aws.String(indexName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
	}
	opts.applyTo(in)
	return in, nil
}

// queryPage runs one page of in and decodes the items and the next-page token.
func (t *Table[T]) queryPage(ctx context.Context, in *dynamodb.QueryInput) ([]*T, *string, error) {
	output, err := dynamodb.NewQueryPaginator(t.client, in).NextPage(ctx)
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

	nextToken, err := encodePageToken(output.LastEvaluatedKey)
	if err != nil {
		return nil, nil, err
	}
	return items, nextToken, nil
}

// countItems sums Count over every page of in, stopping early once maxItems have
// been counted (maxItems <= 0 counts the whole partition).
func (t *Table[T]) countItems(ctx context.Context, in *dynamodb.QueryInput, maxItems int64) (int64, error) {
	in.Select = dynamoTypes.SelectCount

	var total int64
	paginator := dynamodb.NewQueryPaginator(t.client, in)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, errorx.ErrInternal.WithInner(err)
		}
		total += int64(output.Count)
		if maxItems > 0 && total >= maxItems {
			return maxItems, nil
		}
	}
	return total, nil
}

// QueryGSIPage fetches one page of items from a GSI whose partition key attribute equals
// pkValue. An optional skCond narrows results by the GSI sort key; pass nil to match all
// items under the partition key. Pagination tokens are automatically decoded on input and
// encoded on output.
//
// Options tune the query: Descending reverses the sort order and WithFilter adds a
// FilterExpression. ConsistentRead is rejected — a GSI cannot be read consistently.
//
// A filter is applied after the key condition and after pageSize is counted, so a
// filtered page can hold fewer items than pageSize — even none — and still return a
// non-nil next-page token. Keep following the token until it is nil instead of
// treating a short or empty page as the end of the results.
func (t *Table[T]) QueryGSIPage(ctx context.Context, indexName, pkAttr, pkValue string, skCond *SKCondition, pageSize int32, token *string, opts ...QueryOption) ([]*T, *string, error) {
	startKey, err := decodePageToken(token)
	if err != nil {
		return nil, nil, err
	}
	in, err := t.gsiQueryInput(indexName, pkAttr, pkValue, skCond, newQueryOptions(opts))
	if err != nil {
		return nil, nil, err
	}
	in.ExclusiveStartKey = startKey
	in.Limit = aws.Int32(pageSize)
	return t.queryPage(ctx, in)
}

// QueryPage fetches one page of items whose PK matches the pk-tagged field of pkItem,
// optionally filtered by an SK prefix. Pagination tokens are automatically decoded on
// input and encoded on output.
//
// Options tune the query: Descending reverses the sort order, WithFilter adds a
// FilterExpression and ConsistentRead makes the read strongly consistent.
//
// A filter is applied after the key condition and after pageSize is counted, so a
// filtered page can hold fewer items than pageSize — even none — and still return a
// non-nil next-page token. Keep following the token until it is nil instead of
// treating a short or empty page as the end of the results.
func (t *Table[T]) QueryPage(ctx context.Context, pkItem *T, skPrefix string, pageSize int32, token *string, opts ...QueryOption) ([]*T, *string, error) {
	in, err := t.tableQueryInput(pkItem, skPrefix, newQueryOptions(opts))
	if err != nil {
		return nil, nil, err
	}
	startKey, err := decodePageToken(token)
	if err != nil {
		return nil, nil, err
	}
	in.ExclusiveStartKey = startKey
	in.Limit = aws.Int32(pageSize)
	return t.queryPage(ctx, in)
}

// Count returns how many items match the same key condition as QueryPage, without
// fetching them: it pages through the partition with Select=COUNT and sums the
// per-page counts.
//
// WithFilter narrows what is counted and WithMaxItems caps the work — a bounded
// count for a "99+" badge, say. Descending has no effect on a total.
func (t *Table[T]) Count(ctx context.Context, pkItem *T, skPrefix string, opts ...QueryOption) (int64, error) {
	o := newQueryOptions(opts)
	in, err := t.tableQueryInput(pkItem, skPrefix, o)
	if err != nil {
		return 0, err
	}
	return t.countItems(ctx, in, o.maxItems)
}

// CountGSI returns how many items match the same key condition as QueryGSIPage,
// without fetching them. See Count for the available options; ConsistentRead is
// rejected, as it is for QueryGSIPage.
func (t *Table[T]) CountGSI(ctx context.Context, indexName, pkAttr, pkValue string, skCond *SKCondition, opts ...QueryOption) (int64, error) {
	o := newQueryOptions(opts)
	in, err := t.gsiQueryInput(indexName, pkAttr, pkValue, skCond, o)
	if err != nil {
		return 0, err
	}
	return t.countItems(ctx, in, o.maxItems)
}
