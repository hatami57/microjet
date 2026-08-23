package dynamo

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/hatami57/microjet/core/errorx"
)

// QueryOption tunes a query issued by QueryPage, QueryGSIPage, Count or CountGSI.
// Options compose; passing none keeps the historical behaviour — ascending order,
// no filter, eventually consistent reads.
type QueryOption func(*queryOptions)

// queryOptions is the resolved form of the QueryOption list passed to a query.
type queryOptions struct {
	descending bool
	consistent bool
	filter     *expression.ConditionBuilder
	maxItems   int64
}

// Descending returns items in descending sort-key order — newest first when the
// sort key is time-ordered.
func Descending() QueryOption {
	return func(o *queryOptions) { o.descending = true }
}

// WithFilter applies cond as a FilterExpression. A filter is evaluated after the
// key condition and after Limit is counted, so a filtered page can return fewer
// items than the requested page size — or none at all — while still returning a
// next-page token.
//
// Filter attributes are ordinary item attributes, not key attributes:
//
//	page, next, err := table.QueryPage(ctx, &pk, "N:", 20, nil,
//	    dynamo.WithFilter(expression.Name("ReadAt").AttributeNotExists()))
func WithFilter(cond expression.ConditionBuilder) QueryOption {
	return func(o *queryOptions) { o.filter = &cond }
}

// ConsistentRead performs a strongly consistent read. Table queries only:
// DynamoDB does not support consistent reads on a global secondary index, so
// QueryGSIPage and CountGSI reject this option.
func ConsistentRead() QueryOption {
	return func(o *queryOptions) { o.consistent = true }
}

// WithMaxItems stops counting once n items have been accumulated and returns n.
// It is honoured by Count and CountGSI only, and lets a caller put a ceiling on
// an unbounded partition scan — an unread badge that displays "99+", say:
//
//	n, err := table.Count(ctx, &pk, "N:", dynamo.WithMaxItems(100))
func WithMaxItems(n int64) QueryOption {
	return func(o *queryOptions) { o.maxItems = n }
}

// newQueryOptions resolves an option list into a queryOptions value.
func newQueryOptions(opts []QueryOption) *queryOptions {
	o := &queryOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// buildExpression builds the key condition together with the optional filter, so
// the resulting Names()/Values() maps carry the placeholders of both.
func (o *queryOptions) buildExpression(keyEx expression.KeyConditionBuilder) (expression.Expression, error) {
	b := expression.NewBuilder().WithKeyCondition(keyEx)
	if o.filter != nil {
		b = b.WithFilter(*o.filter)
	}
	expr, err := b.Build()
	if err != nil {
		return expression.Expression{}, errorx.ErrInternal.WithInner(err)
	}
	return expr, nil
}

// applyTo sets the input fields the options control. Fields stay untouched when
// the corresponding option was not passed, so an empty option list produces the
// same QueryInput as before options existed.
func (o *queryOptions) applyTo(in *dynamodb.QueryInput) {
	if o.descending {
		in.ScanIndexForward = aws.Bool(false)
	}
	if o.consistent {
		in.ConsistentRead = aws.Bool(true)
	}
}
