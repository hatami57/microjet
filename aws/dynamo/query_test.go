package dynamo

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/google/uuid"
)

// itemPage renders a Query reply holding one item, optionally with a next-page key.
func itemPage(tenantID, userID, id uuid.UUID, title string, more bool) string {
	last := ""
	if more {
		last = fmt.Sprintf(`,"LastEvaluatedKey":{"PK":{"S":"T:%s#U:%s"},"SK":{"S":"N:%s"}}`, tenantID, userID, id)
	}
	return fmt.Sprintf(`{"Count":1,"ScannedCount":1,"Items":[{
		"PK":{"S":"T:%s#U:%s"},"SK":{"S":"N:%s"},"Title":{"S":"%s"},
		"CreatedAt":{"S":"2026-08-23T10:30:00.000Z"},"UpdatedAt":{"S":"2026-08-23T10:30:00.000Z"}}]%s}`,
		tenantID, userID, id, title, last)
}

// countPage renders a Select=COUNT reply, optionally with a next-page key.
func countPage(n int, more bool) string {
	last := ""
	if more {
		last = `,"LastEvaluatedKey":{"PK":{"S":"T:x#U:y"},"SK":{"S":"N:z"}}`
	}
	return fmt.Sprintf(`{"Count":%d,"ScannedCount":%d%s}`, n, n, last)
}

func TestQueryPageDefaultsAreUnchanged(t *testing.T) {
	tenantID, userID, id := uuid.New(), uuid.New(), uuid.New()
	table, stub := newTestTable(t, okResponse(itemPage(tenantID, userID, id, "hello", false)))

	items, next, err := table.QueryPage(context.Background(),
		&testItem{TenantID: tenantID, UserID: userID}, "N:", 20, nil)
	if err != nil {
		t.Fatalf("QueryPage: %v", err)
	}
	if len(items) != 1 || items[0].Title != "hello" {
		t.Fatalf("items = %+v, want one item titled hello", items)
	}
	if items[0].TenantID != tenantID || items[0].ID != id || items[0].UserID != userID {
		t.Errorf("decoded keys = %v/%v/%v, want %v/%v/%v",
			items[0].TenantID, items[0].UserID, items[0].ID, tenantID, userID, id)
	}
	if next != nil {
		t.Errorf("next token = %q, want nil on the last page", *next)
	}

	req := stub.request(t, 0)
	if got := str(t, req, "TableName"); got != "test-table" {
		t.Errorf("TableName = %q", got)
	}
	if got := str(t, req, "KeyConditionExpression"); !strings.Contains(got, "begins_with") {
		t.Errorf("KeyConditionExpression = %q, want the SK prefix condition", got)
	}
	// No options: the request must look exactly as it did before options existed.
	for _, key := range []string{"ScanIndexForward", "FilterExpression", "ConsistentRead", "Select"} {
		if _, ok := req[key]; ok {
			t.Errorf("%s must not be set when no option is passed (got %v)", key, req[key])
		}
	}
	if req["Limit"] != float64(20) {
		t.Errorf("Limit = %v, want 20", req["Limit"])
	}
}

func TestQueryPageOptions(t *testing.T) {
	unread := expression.Name("Unread").Equal(expression.Value("1"))

	tests := []struct {
		name   string
		opts   []QueryOption
		verify func(t *testing.T, req map[string]any)
	}{
		{
			name: "descending",
			opts: []QueryOption{Descending()},
			verify: func(t *testing.T, req map[string]any) {
				if req["ScanIndexForward"] != false {
					t.Errorf("ScanIndexForward = %v, want false", req["ScanIndexForward"])
				}
			},
		},
		{
			name: "filter",
			opts: []QueryOption{WithFilter(unread)},
			verify: func(t *testing.T, req map[string]any) {
				if _, ok := req["FilterExpression"]; !ok {
					t.Fatal("FilterExpression not set")
				}
				// The key placeholders and the filter placeholders have to travel
				// together, or DynamoDB rejects the request as under-specified.
				names := sub(t, req, "ExpressionAttributeNames")
				values := sub(t, req, "ExpressionAttributeValues")
				if !hasValue(names, "PK") || !hasValue(names, "SK") || !hasValue(names, "Unread") {
					t.Errorf("ExpressionAttributeNames = %v, want PK, SK and Unread", names)
				}
				if len(values) != 3 {
					t.Errorf("ExpressionAttributeValues = %v, want the two key values and the filter value", values)
				}
			},
		},
		{
			name: "consistent read",
			opts: []QueryOption{ConsistentRead()},
			verify: func(t *testing.T, req map[string]any) {
				if req["ConsistentRead"] != true {
					t.Errorf("ConsistentRead = %v, want true", req["ConsistentRead"])
				}
			},
		},
		{
			name: "options compose",
			opts: []QueryOption{Descending(), WithFilter(unread), ConsistentRead()},
			verify: func(t *testing.T, req map[string]any) {
				if req["ScanIndexForward"] != false || req["ConsistentRead"] != true {
					t.Errorf("order/consistency lost when composing: %v", req)
				}
				if _, ok := req["FilterExpression"]; !ok {
					t.Error("FilterExpression lost when composing")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			table, stub := newTestTable(t, okResponse(`{"Count":0,"Items":[]}`))
			if _, _, err := table.QueryPage(context.Background(),
				&testItem{TenantID: uuid.New(), UserID: uuid.New()}, "N:", 20, nil, tc.opts...); err != nil {
				t.Fatalf("QueryPage: %v", err)
			}
			tc.verify(t, stub.request(t, 0))
		})
	}
}

// A filter is applied after Limit is counted, so a full-size page request can come
// back short — or empty — and still carry a next-page token. The caller must be able
// to keep paging.
func TestQueryPageFilteredShortPage(t *testing.T) {
	tenantID, userID, id := uuid.New(), uuid.New(), uuid.New()
	unread := expression.Name("Unread").Equal(expression.Value("1"))

	t.Run("short page still returns a token", func(t *testing.T) {
		table, _ := newTestTable(t, okResponse(itemPage(tenantID, userID, id, "hello", true)))
		items, next, err := table.QueryPage(context.Background(),
			&testItem{TenantID: tenantID, UserID: userID}, "N:", 20, nil, WithFilter(unread))
		if err != nil {
			t.Fatalf("QueryPage: %v", err)
		}
		if len(items) != 1 {
			t.Errorf("items = %d, want the single item the filter let through", len(items))
		}
		if next == nil {
			t.Fatal("next token = nil, want a token: the partition is not exhausted")
		}
	})

	t.Run("empty page still returns a token", func(t *testing.T) {
		body := fmt.Sprintf(`{"Count":0,"ScannedCount":20,"Items":[],"LastEvaluatedKey":{"PK":{"S":"T:%s#U:%s"},"SK":{"S":"N:%s"}}}`,
			tenantID, userID, id)
		table, _ := newTestTable(t, okResponse(body))
		items, next, err := table.QueryPage(context.Background(),
			&testItem{TenantID: tenantID, UserID: userID}, "N:", 20, nil, WithFilter(unread))
		if err != nil {
			t.Fatalf("QueryPage: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("items = %d, want 0", len(items))
		}
		if next == nil {
			t.Fatal("next token = nil, want a token even though the page is empty")
		}
	})
}

// The returned token has to survive a round trip back into the next call.
func TestQueryPageTokenRoundTrip(t *testing.T) {
	tenantID, userID, id := uuid.New(), uuid.New(), uuid.New()
	table, stub := newTestTable(t,
		okResponse(itemPage(tenantID, userID, id, "first", true)),
		okResponse(itemPage(tenantID, userID, id, "second", false)),
	)
	pk := &testItem{TenantID: tenantID, UserID: userID}

	_, next, err := table.QueryPage(context.Background(), pk, "N:", 20, nil)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if next == nil {
		t.Fatal("first page returned no token")
	}
	if _, _, err = table.QueryPage(context.Background(), pk, "N:", 20, next); err != nil {
		t.Fatalf("second page: %v", err)
	}

	start := sub(t, stub.request(t, 1), "ExclusiveStartKey")
	if got := str(t, sub(t, start, "SK"), "S"); got != "N:"+id.String() {
		t.Errorf("ExclusiveStartKey SK = %q, want the previous page's last key", got)
	}
}

func TestQueryPageRejectsMalformedToken(t *testing.T) {
	table, stub := newTestTable(t)
	bad := "not-base64!"
	if _, _, err := table.QueryPage(context.Background(),
		&testItem{TenantID: uuid.New()}, "N:", 20, &bad); err == nil {
		t.Fatal("expected an error for a malformed page token")
	}
	if len(stub.requests) != 0 {
		t.Errorf("made %d request(s); a malformed token must fail before the call", len(stub.requests))
	}
}

func TestQueryGSIPageOptions(t *testing.T) {
	table, stub := newTestTable(t, okResponse(`{"Count":0,"Items":[]}`))

	_, _, err := table.QueryGSIPage(context.Background(), "GSI1", "GSI1PK", "T:acme",
		SKBeginsWith("GSI1SK", "N:"), 10, nil,
		Descending(), WithFilter(expression.Name("Unread").Equal(expression.Value("1"))))
	if err != nil {
		t.Fatalf("QueryGSIPage: %v", err)
	}

	req := stub.request(t, 0)
	if got := str(t, req, "IndexName"); got != "GSI1" {
		t.Errorf("IndexName = %q", got)
	}
	if req["ScanIndexForward"] != false {
		t.Errorf("ScanIndexForward = %v, want false", req["ScanIndexForward"])
	}
	if _, ok := req["FilterExpression"]; !ok {
		t.Error("FilterExpression not set")
	}
}

// DynamoDB cannot serve a consistent read from a GSI, so the option is rejected
// locally instead of failing at request time.
func TestQueryGSIPageRejectsConsistentRead(t *testing.T) {
	table, stub := newTestTable(t)

	_, _, err := table.QueryGSIPage(context.Background(), "GSI1", "GSI1PK", "T:acme", nil, 10, nil, ConsistentRead())
	if err == nil {
		t.Fatal("expected an error for a consistent read on a GSI")
	}
	if !strings.Contains(err.Error(), "ConsistentRead") {
		t.Errorf("error = %v, want it to name the offending option", err)
	}
	if len(stub.requests) != 0 {
		t.Errorf("made %d request(s); the option must be rejected before the call", len(stub.requests))
	}

	if _, err = table.CountGSI(context.Background(), "GSI1", "GSI1PK", "T:acme", nil, ConsistentRead()); err == nil {
		t.Fatal("CountGSI: expected an error for a consistent read on a GSI")
	}
}

func TestCountSumsEveryPage(t *testing.T) {
	table, stub := newTestTable(t,
		okResponse(countPage(50, true)),
		okResponse(countPage(30, true)),
		okResponse(countPage(7, false)),
	)

	got, err := table.Count(context.Background(), &testItem{TenantID: uuid.New(), UserID: uuid.New()}, "N:")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if want := int64(87); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
	if len(stub.requests) != 3 {
		t.Errorf("made %d request(s), want 3 — one per page", len(stub.requests))
	}
	if sel := str(t, stub.request(t, 0), "Select"); sel != "COUNT" {
		t.Errorf("Select = %q, want COUNT", sel)
	}
	if _, ok := stub.request(t, 0)["Limit"]; ok {
		t.Error("Count must not cap the page size")
	}
}

func TestCountWithMaxItems(t *testing.T) {
	tests := []struct {
		name         string
		max          int64
		pages        []stubResponse
		want         int64
		wantRequests int
	}{
		{
			name:         "cap reached on the first page",
			max:          40,
			pages:        []stubResponse{okResponse(countPage(50, true)), okResponse(countPage(30, false))},
			want:         40,
			wantRequests: 1,
		},
		{
			name:         "cap reached after two pages",
			max:          60,
			pages:        []stubResponse{okResponse(countPage(50, true)), okResponse(countPage(30, true))},
			want:         60,
			wantRequests: 2,
		},
		{
			name:         "cap never reached",
			max:          100,
			pages:        []stubResponse{okResponse(countPage(5, true)), okResponse(countPage(3, false))},
			want:         8,
			wantRequests: 2,
		},
		{
			name:         "exact cap stops paginating",
			max:          50,
			pages:        []stubResponse{okResponse(countPage(50, true)), okResponse(countPage(1, false))},
			want:         50,
			wantRequests: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			table, stub := newTestTable(t, tc.pages...)
			got, err := table.Count(context.Background(),
				&testItem{TenantID: uuid.New(), UserID: uuid.New()}, "N:", WithMaxItems(tc.max))
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if got != tc.want {
				t.Errorf("Count = %d, want %d", got, tc.want)
			}
			if len(stub.requests) != tc.wantRequests {
				t.Errorf("made %d request(s), want %d", len(stub.requests), tc.wantRequests)
			}
		})
	}
}

func TestCountGSIAppliesFilter(t *testing.T) {
	table, stub := newTestTable(t, okResponse(countPage(3, false)))

	got, err := table.CountGSI(context.Background(), "GSI1", "GSI1PK", "T:acme",
		SKBeginsWith("GSI1SK", "N:"),
		WithFilter(expression.Name("Unread").Equal(expression.Value("1"))))
	if err != nil {
		t.Fatalf("CountGSI: %v", err)
	}
	if got != 3 {
		t.Errorf("CountGSI = %d, want 3", got)
	}

	req := stub.request(t, 0)
	if sel := str(t, req, "Select"); sel != "COUNT" {
		t.Errorf("Select = %q, want COUNT", sel)
	}
	if got := str(t, req, "IndexName"); got != "GSI1" {
		t.Errorf("IndexName = %q", got)
	}
	if _, ok := req["FilterExpression"]; !ok {
		t.Error("FilterExpression not set")
	}
}

// hasValue reports whether m maps some placeholder to want.
func hasValue(m map[string]any, want string) bool {
	for _, v := range m {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}
