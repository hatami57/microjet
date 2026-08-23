package dynamo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/hatami57/microjet/core/errorx"
)

// attrS returns the string held by the named attribute value.
func attrS(t *testing.T, av map[string]dynamoTypes.AttributeValue, name string) string {
	t.Helper()
	v, ok := av[name]
	if !ok {
		t.Fatalf("attribute %q missing from %v", name, av)
	}
	s, ok := v.(*dynamoTypes.AttributeValueMemberS)
	if !ok {
		t.Fatalf("attribute %q is %T, want a string", name, v)
	}
	return s.Value
}

func TestPutTxBuildsTheSameItemAsPut(t *testing.T) {
	table, stub := newTestTable(t, okResponse(`{}`))
	item := newTestItem()

	// Put and PutTx must marshal identically — same keys, same timestamps.
	if err := table.Put(context.Background(), item); err != nil {
		t.Fatalf("Put: %v", err)
	}
	write, err := table.PutTx(newTestItemFrom(item), nil)
	if err != nil {
		t.Fatalf("PutTx: %v", err)
	}
	if write.Put == nil {
		t.Fatal("PutTx did not build a Put")
	}
	if got := aws.ToString(write.Put.TableName); got != "test-table" {
		t.Errorf("TableName = %q", got)
	}

	putItem := sub(t, stub.request(t, 0), "Item")
	for _, name := range []string{"PK", "SK", "Title", "CreatedAt", "UpdatedAt"} {
		want := str(t, sub(t, putItem, name), "S")
		if got := attrS(t, write.Put.Item, name); got != want {
			t.Errorf("%s: PutTx wrote %q, Put wrote %q", name, got, want)
		}
	}
	if write.Put.ConditionExpression != nil {
		t.Errorf("unconditional PutTx set a condition: %q", *write.Put.ConditionExpression)
	}
}

func TestPutTxWithCondition(t *testing.T) {
	table, _ := newTestTable(t)
	cond := expression.AttributeNotExists(expression.Name("PK"))

	write, err := table.PutTx(newTestItem(), &cond)
	if err != nil {
		t.Fatalf("PutTx: %v", err)
	}
	if write.Put.ConditionExpression == nil {
		t.Fatal("ConditionExpression not set")
	}
	if !strings.Contains(*write.Put.ConditionExpression, "attribute_not_exists") {
		t.Errorf("ConditionExpression = %q", *write.Put.ConditionExpression)
	}
	if len(write.Put.ExpressionAttributeNames) == 0 {
		t.Error("ExpressionAttributeNames not carried over")
	}
}

func TestUpdateTx(t *testing.T) {
	table, _ := newTestTable(t)
	item := newTestItem()
	cond := expression.Name("Unread").Equal(expression.Value("1"))

	write, err := table.UpdateTx(item, UpdateSpec{
		Set:       []string{"Title"},
		Remove:    []string{"Unread"},
		Condition: &cond,
	})
	if err != nil {
		t.Fatalf("UpdateTx: %v", err)
	}
	if write.Update == nil {
		t.Fatal("UpdateTx did not build an Update")
	}
	expr := aws.ToString(write.Update.UpdateExpression)
	if !strings.Contains(expr, "SET") || !strings.Contains(expr, "REMOVE") {
		t.Errorf("UpdateExpression = %q, want both clauses", expr)
	}
	if write.Update.ConditionExpression == nil {
		t.Error("ConditionExpression not set")
	}
	// The key is built exactly as the non-transactional path builds it.
	if got := attrS(t, write.Update.Key, "SK"); got != "N:"+item.ID.String() {
		t.Errorf("SK = %q", got)
	}
	if !item.UpdatedAt.Time().Equal(fixedNow) {
		t.Errorf("UpdatedAt = %v, want the auto_update timestamp", item.UpdatedAt.Time())
	}
}

func TestUpdateTxRejectsAnEmptySpec(t *testing.T) {
	table, _ := newTestTableNoAuto(t)

	if _, err := table.UpdateTx(&plainItem{ID: "a"}, UpdateSpec{}); err == nil {
		t.Fatal("expected an error: a transaction item cannot be empty")
	}
	if _, err := table.UpdateTx(&plainItem{ID: "a"}, UpdateSpec{Set: []string{"Name"}, Remove: []string{"Name"}}); err == nil {
		t.Fatal("expected the SET/REMOVE overlap to be rejected in a transaction too")
	}
}

func TestDeleteTxAndConditionCheckTx(t *testing.T) {
	table, _ := newTestTable(t)
	item := newTestItem()
	cond := expression.AttributeExists(expression.Name("PK"))

	del, err := table.DeleteTx(item, &cond)
	if err != nil {
		t.Fatalf("DeleteTx: %v", err)
	}
	if del.Delete == nil {
		t.Fatal("DeleteTx did not build a Delete")
	}
	if got := attrS(t, del.Delete.Key, "PK"); got != "T:"+item.TenantID.String()+"#U:"+item.UserID.String() {
		t.Errorf("PK = %q, want the composite key", got)
	}
	if del.Delete.ConditionExpression == nil {
		t.Error("ConditionExpression not set")
	}

	unconditional, err := table.DeleteTx(item, nil)
	if err != nil {
		t.Fatalf("DeleteTx: %v", err)
	}
	if unconditional.Delete.ConditionExpression != nil {
		t.Error("unconditional DeleteTx set a condition")
	}

	check, err := table.ConditionCheckTx(item, cond)
	if err != nil {
		t.Fatalf("ConditionCheckTx: %v", err)
	}
	if check.ConditionCheck == nil {
		t.Fatal("ConditionCheckTx did not build a ConditionCheck")
	}
	if check.ConditionCheck.ConditionExpression == nil {
		t.Error("ConditionExpression not set")
	}
	if got := attrS(t, check.ConditionCheck.Key, "SK"); got != "N:"+item.ID.String() {
		t.Errorf("SK = %q", got)
	}
}

func TestTransactWrite(t *testing.T) {
	table, stub := newTestTable(t, okResponse(`{}`))
	put, err := table.PutTx(newTestItem(), nil)
	if err != nil {
		t.Fatalf("PutTx: %v", err)
	}
	del, err := table.DeleteTx(newTestItem(), nil)
	if err != nil {
		t.Fatalf("DeleteTx: %v", err)
	}

	if err := TransactWrite(context.Background(), table.client, put, del); err != nil {
		t.Fatalf("TransactWrite: %v", err)
	}
	req := stub.request(t, 0)
	items, ok := req["TransactItems"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("TransactItems = %v, want 2 items", req["TransactItems"])
	}
	if !strings.HasSuffix(stub.targets[0], "TransactWriteItems") {
		t.Errorf("called %q, want TransactWriteItems", stub.targets[0])
	}
}

func TestTransactWriteEmptyBatchIsANoOp(t *testing.T) {
	table, stub := newTestTable(t)

	if err := TransactWrite(context.Background(), table.client); err != nil {
		t.Fatalf("TransactWrite: %v", err)
	}
	if len(stub.requests) != 0 {
		t.Errorf("made %d request(s), want none", len(stub.requests))
	}
}

func TestTransactWriteRejectsOversizedBatch(t *testing.T) {
	table, stub := newTestTable(t)
	item, err := table.PutTx(newTestItem(), nil)
	if err != nil {
		t.Fatalf("PutTx: %v", err)
	}

	atLimit := make([]dynamoTypes.TransactWriteItem, maxTransactItems)
	for i := range atLimit {
		atLimit[i] = item
	}
	// 100 items is allowed and goes out; 101 is rejected before the call.
	if err := TransactWrite(context.Background(), table.client, atLimit...); err != nil {
		t.Fatalf("TransactWrite at the limit: %v", err)
	}
	if len(stub.requests) != 1 {
		t.Fatalf("made %d request(s), want 1", len(stub.requests))
	}

	tooMany := append(atLimit, item) //nolint:gocritic // a copy is intended
	err = TransactWrite(context.Background(), table.client, tooMany...)
	if err == nil {
		t.Fatal("expected an error for a batch over the 100-item limit")
	}
	if !errorx.IsBadRequestError(err) {
		t.Errorf("error = %v, want a bad-request error", err)
	}
	if len(stub.requests) != 1 {
		t.Errorf("made %d request(s); an oversized batch must not be sent", len(stub.requests))
	}
}

// A cancelled transaction has to say which item failed and why: a bare
// "transaction cancelled" is undebuggable in a multi-item write.
func TestTransactWriteUnpacksCancellationReasons(t *testing.T) {
	body := `{"__type":"com.amazonaws.dynamodb.v20120810#TransactionCanceledException",
		"Message":"Transaction cancelled, please refer cancellation reasons for specific reasons",
		"CancellationReasons":[{"Code":"None"},
		{"Code":"ConditionalCheckFailed","Message":"The conditional request failed"}]}`
	table, _ := newTestTable(t, errResponse("TransactionCanceledException", body))
	put, err := table.PutTx(newTestItem(), nil)
	if err != nil {
		t.Fatalf("PutTx: %v", err)
	}

	err = TransactWrite(context.Background(), table.client, put, put)
	if err == nil {
		t.Fatal("expected an error for a cancelled transaction")
	}
	if !errors.Is(err, ErrConditionFailed) {
		t.Errorf("error = %v, want it to match ErrConditionFailed", err)
	}
	if !strings.Contains(err.Error(), "item 1") {
		t.Errorf("error = %v, want it to name the failing item index", err)
	}
	if !strings.Contains(err.Error(), "ConditionalCheckFailed") {
		t.Errorf("error = %v, want it to name the cancellation reason", err)
	}
}

func TestMapWriteError(t *testing.T) {
	reason := func(code, msg string) dynamoTypes.CancellationReason {
		r := dynamoTypes.CancellationReason{Code: aws.String(code)}
		if msg != "" {
			r.Message = aws.String(msg)
		}
		return r
	}

	tests := []struct {
		name          string
		err           error
		wantCondition bool
		wantContains  []string
	}{
		{
			name:          "conditional check failure",
			err:           &dynamoTypes.ConditionalCheckFailedException{Message: aws.String("nope")},
			wantCondition: true,
		},
		{
			name: "cancelled by a condition",
			err: &dynamoTypes.TransactionCanceledException{
				Message: aws.String("cancelled"),
				CancellationReasons: []dynamoTypes.CancellationReason{
					reason("None", ""),
					reason("ConditionalCheckFailed", "the conditional request failed"),
				},
			},
			wantCondition: true,
			wantContains:  []string{"item 1", "ConditionalCheckFailed", "the conditional request failed"},
		},
		{
			name: "cancelled for another reason",
			err: &dynamoTypes.TransactionCanceledException{
				Message: aws.String("cancelled"),
				CancellationReasons: []dynamoTypes.CancellationReason{
					reason("None", ""),
					reason("None", ""),
					reason("ThrottlingError", "throughput exceeded"),
				},
			},
			wantCondition: false,
			wantContains:  []string{"item 2", "ThrottlingError"},
		},
		{
			name:          "anything else stays internal",
			err:           errors.New("connection reset"),
			wantCondition: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapWriteError(tc.err)
			if isCond := errors.Is(got, ErrConditionFailed); isCond != tc.wantCondition {
				t.Errorf("matches ErrConditionFailed = %v, want %v (error: %v)", isCond, tc.wantCondition, got)
			}
			if !tc.wantCondition && !errorx.IsInternalError(got) {
				t.Errorf("error = %v, want an internal error", got)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got.Error(), want) {
					t.Errorf("error = %v, want it to mention %q", got, want)
				}
			}
			// The AWS error stays reachable underneath.
			if !errors.Is(got, tc.err) && errors.Unwrap(got) != tc.err {
				t.Errorf("error = %v, want it to wrap the AWS error", got)
			}
		})
	}
}

// newTestItemFrom copies an item so two builders can be given equal input.
func newTestItemFrom(src *testItem) *testItem {
	clone := *src
	return &clone
}
