package dynamo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core/errorx"
)

func newTestItem() *testItem {
	return &testItem{
		TenantID: uuid.New(),
		UserID:   uuid.New(),
		ID:       uuid.New(),
		Title:    "hello",
		Unread:   "1",
	}
}

func TestUpdateWithExpressions(t *testing.T) {
	tests := []struct {
		name       string
		spec       UpdateSpec
		wantSet    []string // attribute names expected in the SET clause
		wantRemove []string // attribute names expected in the REMOVE clause
	}{
		{
			name:    "set only",
			spec:    UpdateSpec{Set: []string{"Title"}},
			wantSet: []string{"Title", "UpdatedAt"}, // auto_update is always included
		},
		{
			name:       "remove only",
			spec:       UpdateSpec{Remove: []string{"Unread"}},
			wantSet:    []string{"UpdatedAt"},
			wantRemove: []string{"Unread"},
		},
		{
			name:       "set and remove together",
			spec:       UpdateSpec{Set: []string{"Title"}, Remove: []string{"Unread"}},
			wantSet:    []string{"Title", "UpdatedAt"},
			wantRemove: []string{"Unread"},
		},
		{
			name:       "duplicates collapse",
			spec:       UpdateSpec{Set: []string{"Title", "Title"}, Remove: []string{"Unread", "Unread"}},
			wantSet:    []string{"Title", "UpdatedAt"},
			wantRemove: []string{"Unread"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			table, stub := newTestTable(t, okResponse(`{}`))
			if err := table.UpdateWith(context.Background(), newTestItem(), tc.spec); err != nil {
				t.Fatalf("UpdateWith: %v", err)
			}

			req := stub.request(t, 0)
			expr := str(t, req, "UpdateExpression")
			names := sub(t, req, "ExpressionAttributeNames")
			setClause, removeClause := splitUpdateExpression(t, expr)

			for _, want := range tc.wantSet {
				if !clauseHasAttribute(setClause, names, want) {
					t.Errorf("SET clause %q does not write %s (names %v)", setClause, want, names)
				}
			}
			for _, want := range tc.wantRemove {
				if !clauseHasAttribute(removeClause, names, want) {
					t.Errorf("REMOVE clause %q does not drop %s (names %v)", removeClause, want, names)
				}
			}
			if len(tc.wantRemove) == 0 && removeClause != "" {
				t.Errorf("unexpected REMOVE clause %q", removeClause)
			}
			// The REMOVEd attribute must not also be sent as a value.
			for _, name := range tc.wantRemove {
				if clauseHasAttribute(setClause, names, name) {
					t.Errorf("%s appears in both SET and REMOVE: %q", name, expr)
				}
			}
		})
	}
}

// DynamoDB rejects an expression that both SETs and REMOVEs an attribute; catching
// it locally saves a pointless round trip and gives a clearer error.
func TestUpdateWithRejectsSetRemoveOverlap(t *testing.T) {
	tests := []struct {
		name string
		spec UpdateSpec
	}{
		{
			name: "explicit overlap",
			spec: UpdateSpec{Set: []string{"Title", "Unread"}, Remove: []string{"Unread"}},
		},
		{
			name: "overlap with an auto_update field",
			spec: UpdateSpec{Set: []string{"Title"}, Remove: []string{"UpdatedAt"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			table, stub := newTestTable(t)
			err := table.UpdateWith(context.Background(), newTestItem(), tc.spec)
			if err == nil {
				t.Fatal("expected an error for an attribute that is both SET and REMOVEd")
			}
			if !errorx.IsBadRequestError(err) {
				t.Errorf("error = %v, want a bad-request validation error", err)
			}
			if len(stub.requests) != 0 {
				t.Errorf("made %d request(s); the overlap must be caught locally", len(stub.requests))
			}
		})
	}
}

func TestUpdateWithEmptySpecMakesNoRequest(t *testing.T) {
	table, stub := newTestTableNoAuto(t)

	if err := table.UpdateWith(context.Background(), &plainItem{ID: "a", Name: "n"}, UpdateSpec{}); err != nil {
		t.Fatalf("UpdateWith: %v", err)
	}
	// An unknown attribute name matches nothing on the item, so there is still
	// nothing to write.
	if err := table.UpdateWith(context.Background(), &plainItem{ID: "a"}, UpdateSpec{Set: []string{"Nope"}}); err != nil {
		t.Fatalf("UpdateWith: %v", err)
	}
	if len(stub.requests) != 0 {
		t.Errorf("made %d request(s), want none for an empty update", len(stub.requests))
	}
}

func TestUpdateWithRemoveOnlyIsValid(t *testing.T) {
	table, stub := newTestTableNoAuto(t, okResponse(`{}`))

	// No auto_update field and no Set: the update is a pure REMOVE.
	if err := table.UpdateWith(context.Background(), &plainItem{ID: "a"}, UpdateSpec{Remove: []string{"Name"}}); err != nil {
		t.Fatalf("UpdateWith: %v", err)
	}
	req := stub.request(t, 0)
	expr := str(t, req, "UpdateExpression")
	if !strings.HasPrefix(strings.TrimSpace(expr), "REMOVE") {
		t.Errorf("UpdateExpression = %q, want a REMOVE-only expression", expr)
	}
	if _, ok := req["ExpressionAttributeValues"]; ok {
		t.Errorf("a REMOVE-only update must not carry values: %v", req["ExpressionAttributeValues"])
	}
}

func TestUpdateWithCondition(t *testing.T) {
	table, stub := newTestTable(t, okResponse(`{}`))
	cond := expression.Name("Unread").Equal(expression.Value("1"))

	err := table.UpdateWith(context.Background(), newTestItem(),
		UpdateSpec{Set: []string{"Title"}, Condition: &cond})
	if err != nil {
		t.Fatalf("UpdateWith: %v", err)
	}

	req := stub.request(t, 0)
	if _, ok := req["ConditionExpression"]; !ok {
		t.Fatal("ConditionExpression not set")
	}
	// Condition and update placeholders share one Names/Values map.
	names := sub(t, req, "ExpressionAttributeNames")
	if !hasValue(names, "Unread") || !hasValue(names, "Title") {
		t.Errorf("ExpressionAttributeNames = %v, want both the condition and the update names", names)
	}
}

// A failed condition is an expected outcome, not an internal error: callers match
// it with errors.Is instead of inspecting the message.
func TestUpdateWithConditionalCheckFailureIsTyped(t *testing.T) {
	table, _ := newTestTable(t, errResponse("ConditionalCheckFailedException",
		`{"__type":"com.amazonaws.dynamodb.v20120810#ConditionalCheckFailedException","message":"The conditional request failed"}`))
	cond := expression.Name("Unread").Equal(expression.Value("1"))

	err := table.UpdateWith(context.Background(), newTestItem(),
		UpdateSpec{Set: []string{"Title"}, Condition: &cond})
	if err == nil {
		t.Fatal("expected an error when the condition does not hold")
	}
	if !errors.Is(err, ErrConditionFailed) {
		t.Errorf("error = %v, want it to match ErrConditionFailed", err)
	}
	if errors.Is(err, errorx.ErrInternal) {
		t.Error("a failed condition must not be reported as an internal error")
	}
	// The AWS exception stays reachable for callers that want the details.
	if _, ok := errors.AsType[*dynamoTypes.ConditionalCheckFailedException](err); !ok {
		t.Error("the underlying AWS exception was dropped")
	}
}

func TestUpdateWithOtherFailuresStayInternal(t *testing.T) {
	table, _ := newTestTable(t, errResponse("InternalServerError",
		`{"__type":"com.amazonaws.dynamodb.v20120810#InternalServerError","message":"boom"}`))

	err := table.UpdateWith(context.Background(), newTestItem(), UpdateSpec{Set: []string{"Title"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrConditionFailed) {
		t.Errorf("error = %v, want an internal error, not a condition failure", err)
	}
	if !errorx.IsInternalError(err) {
		t.Errorf("error = %v, want an internal error", err)
	}
}

// Update is now a thin wrapper over UpdateWith; its behaviour must not have moved.
func TestUpdateStillSetsOnlyTheNamedAndAutoFields(t *testing.T) {
	table, stub := newTestTable(t, okResponse(`{}`))
	item := newTestItem()

	if err := table.Update(context.Background(), item, "Title"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	req := stub.request(t, 0)
	expr := str(t, req, "UpdateExpression")
	names := sub(t, req, "ExpressionAttributeNames")
	if strings.Contains(expr, "REMOVE") {
		t.Errorf("UpdateExpression = %q, want SET only", expr)
	}
	if _, ok := req["ConditionExpression"]; ok {
		t.Error("Update must not send a condition")
	}
	for _, want := range []string{"Title", "UpdatedAt"} {
		if !hasValue(names, want) {
			t.Errorf("%s missing from the update (names %v)", want, names)
		}
	}
	if hasValue(names, "CreatedAt") {
		t.Error("auto_create fields must not be rewritten by an update")
	}
	// The key is built from the item's pk/sk fields.
	key := sub(t, req, "Key")
	if got := str(t, sub(t, key, "SK"), "S"); got != "N:"+item.ID.String() {
		t.Errorf("SK = %q, want N:%s", got, item.ID)
	}
	if got := str(t, sub(t, key, "PK"), "S"); got != "T:"+item.TenantID.String()+"#U:"+item.UserID.String() {
		t.Errorf("PK = %q, want the composite key", got)
	}
}

func TestUpdateAppliesAutoUpdateTimestamp(t *testing.T) {
	table, _ := newTestTable(t, okResponse(`{}`))
	item := newTestItem()

	if err := table.Update(context.Background(), item, "Title"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !item.UpdatedAt.Time().Equal(fixedNow) {
		t.Errorf("UpdatedAt = %v, want the clock's time %v", item.UpdatedAt.Time(), fixedNow)
	}
	if !item.CreatedAt.Time().IsZero() {
		t.Errorf("CreatedAt = %v, want it untouched by an update", item.CreatedAt.Time())
	}
}

// splitUpdateExpression pulls the SET and REMOVE clauses out of an UpdateExpression.
// The SDK decides the clause order, so neither position is assumed.
func splitUpdateExpression(t *testing.T, expr string) (setClause, removeClause string) {
	t.Helper()
	setAt := strings.Index(expr, "SET ")
	removeAt := strings.Index(expr, "REMOVE ")

	// Each clause runs until the next keyword, or to the end of the expression.
	cut := func(start int, others ...int) string {
		if start < 0 {
			return ""
		}
		end := len(expr)
		for _, o := range others {
			if o > start && o < end {
				end = o
			}
		}
		return strings.TrimSpace(expr[start:end])
	}
	return cut(setAt, removeAt), cut(removeAt, setAt)
}

// clauseHasAttribute reports whether clause references the attribute called name,
// resolving the #0-style placeholders through the names map.
func clauseHasAttribute(clause string, names map[string]any, name string) bool {
	for placeholder, actual := range names {
		if s, ok := actual.(string); ok && s == name {
			return strings.Contains(clause, placeholder)
		}
	}
	return false
}
