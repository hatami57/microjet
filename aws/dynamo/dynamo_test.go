package dynamo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
)

// testItem is the item type the table tests operate on: a composite-key notification
// with a sparse GSI attribute that a REMOVE can clear.
type testItem struct {
	TenantID  uuid.UUID `dynamo:"pk,format=T:{TenantID}#U:{UserID}" dynamodbav:"-"`
	UserID    uuid.UUID `dynamodbav:"-"`
	ID        uuid.UUID `dynamo:"sk,prefix=N:"                     dynamodbav:"-"`
	Title     string    `dynamodbav:"Title"`
	Unread    string    `dynamodbav:"Unread,omitempty"`
	CreatedAt Timestamp `dynamo:"auto_create"                      dynamodbav:"CreatedAt"`
	UpdatedAt Timestamp `dynamo:"auto_update"                      dynamodbav:"UpdatedAt"`
}

// fixedNow is the time the test clock reports, so marshalled timestamps are stable.
var fixedNow = time.Date(2026, 8, 23, 10, 30, 0, 0, time.UTC)

// stubResponse is one canned HTTP reply for the stub client.
type stubResponse struct {
	status  int
	body    string
	errType string // exception name, for error replies
}

// okResponse is a 200 with the given JSON body.
func okResponse(body string) stubResponse { return stubResponse{status: 200, body: body} }

// errResponse is a DynamoDB error reply: the SDK resolves the exception type from
// the X-Amzn-ErrorType header and deserialises the body into the typed exception.
func errResponse(errType, body string) stubResponse {
	return stubResponse{status: 400, body: body, errType: errType}
}

// stubHTTP is a dynamodb.HTTPClient that records every request and replays canned
// responses in order, so a test can assert on the exact wire request the Table built.
type stubHTTP struct {
	responses []stubResponse
	requests  []map[string]any
	targets   []string
	next      int
}

func (s *stubHTTP) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
	}
	s.requests = append(s.requests, decoded)
	s.targets = append(s.targets, req.Header.Get("X-Amz-Target"))

	resp := stubResponse{status: 200, body: "{}"}
	if s.next < len(s.responses) {
		resp = s.responses[s.next]
	}
	s.next++

	header := http.Header{"Content-Type": []string{"application/x-amz-json-1.0"}}
	if resp.errType != "" {
		header.Set("X-Amzn-ErrorType", resp.errType)
	}
	return &http.Response{
		StatusCode:    resp.status,
		Status:        http.StatusText(resp.status),
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(resp.body)),
		ContentLength: int64(len(resp.body)),
		Request:       req,
	}, nil
}

// request returns the decoded body of the i-th request, failing the test if the
// client never made it.
func (s *stubHTTP) request(t *testing.T, i int) map[string]any {
	t.Helper()
	if i >= len(s.requests) {
		t.Fatalf("expected at least %d request(s), got %d", i+1, len(s.requests))
	}
	return s.requests[i]
}

// plainItem has no auto_* fields, so an update writes exactly what the spec names.
type plainItem struct {
	ID   string `dynamo:"pk,prefix=P:"  dynamodbav:"-"`
	Kind string `dynamo:"sk,const=META" dynamodbav:"-"`
	Name string `dynamodbav:"Name"`
}

// newStubClient builds a dynamodb.Client that talks to a stub HTTP client instead
// of AWS. Retries are off so one canned response is consumed per call.
func newStubClient(responses []stubResponse) (*dynamodb.Client, *stubHTTP) {
	stub := &stubHTTP{responses: responses}
	client := dynamodb.New(dynamodb.Options{
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
		HTTPClient:       stub,
		RetryMaxAttempts: 1,
	})
	return client, stub
}

// newTestTable builds a testItem Table wired to a stub HTTP client and a fixed clock.
func newTestTable(t *testing.T, responses ...stubResponse) (*Table[testItem], *stubHTTP) {
	t.Helper()
	client, stub := newStubClient(responses)
	table, err := New[testItem](client, "test-table")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	table.clock = core.NewFixedClock(fixedNow)
	return table, stub
}

// newTestTableNoAuto builds a Table for an item type without auto_* fields.
func newTestTableNoAuto(t *testing.T, responses ...stubResponse) (*Table[plainItem], *stubHTTP) {
	t.Helper()
	client, stub := newStubClient(responses)
	table, err := New[plainItem](client, "test-table")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	table.clock = core.NewFixedClock(fixedNow)
	return table, stub
}

// str returns the string at key, failing the test when it is missing.
func str(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("key %q missing from %v", key, m)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("key %q is %T, want string", key, v)
	}
	return s
}

// sub returns the nested object at key, failing the test when it is missing.
func sub(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("key %q missing from %v", key, m)
	}
	o, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("key %q is %T, want object", key, v)
	}
	return o
}
