package dynamo

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
)

// derivedItem is the shape const= and format= exist for: a type discriminator and a
// pair of GSI key attributes that nobody has to remember to fill in.
type derivedItem struct {
	TenantID uuid.UUID `dynamo:"pk,format=T:{TenantID}#U:{UserID}" dynamodbav:"-"`
	UserID   uuid.UUID `dynamodbav:"-"`
	ID       code      `dynamo:"sk,prefix=M:"                     dynamodbav:"-"`

	Type   string `dynamo:"const=MESSAGE"           dynamodbav:"Type"`
	Title  string `dynamodbav:"Title"`
	GSI1PK string `dynamo:"format=T:{TenantID}#MSG" dynamodbav:"GSI1_PK,omitempty"`
	GSI1SK string `dynamo:"format=M:{ID}"           dynamodbav:"GSI1_SK,omitempty"`
}

func newDerivedItem() *derivedItem {
	return &derivedItem{
		TenantID: uuid.MustParse("6f0e3f1e-6a8f-4f2e-9a3d-2f5c9b1d7e21"),
		UserID:   uuid.MustParse("2b1c8a44-9d3e-4f7a-8c21-5e6b0a9d4f38"),
		ID:       code{0x01, 0x02, 0x03, 0x04},
		Title:    "hello",
	}
}

// newDerivedTable builds a derivedItem Table wired to a stub HTTP client.
func newDerivedTable(t *testing.T, responses ...stubResponse) (*Table[derivedItem], *stubHTTP) {
	t.Helper()
	client, stub := newStubClient(responses)
	table, err := New[derivedItem](client, "test-table")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	table.clock = core.NewFixedClock(fixedNow)
	return table, stub
}

// A Put fills in every derived attribute from the item's own fields.
func TestPutWritesDerivedAttributes(t *testing.T) {
	table, _ := newDerivedTable(t)
	item := newDerivedItem()

	av, err := table.marshalItem(item)
	if err != nil {
		t.Fatalf("marshalItem: %v", err)
	}

	want := map[string]string{
		"Type":    "MESSAGE",
		"GSI1_PK": "T:" + item.TenantID.String() + "#MSG",
		"GSI1_SK": "M:01020304",
	}
	for name, w := range want {
		if got := attrS(t, av, name); got != w {
			t.Errorf("%s = %q, want %q", name, got, w)
		}
	}
	// The item the caller holds is updated too, so it matches what was stored.
	if item.GSI1PK != want["GSI1_PK"] || item.Type != "MESSAGE" {
		t.Errorf("item not updated in place: %+v", item)
	}
}

// A derived attribute is recomputed on every write, so a value the caller left stale
// (or filled in by hand and got wrong) cannot reach the table.
func TestPutRecomputesStaleDerivedAttributes(t *testing.T) {
	table, _ := newDerivedTable(t)
	item := newDerivedItem()
	item.Type = "NOT-A-MESSAGE"
	item.GSI1PK = "T:someone-else#MSG"
	item.GSI1SK = ""

	av, err := table.marshalItem(item)
	if err != nil {
		t.Fatalf("marshalItem: %v", err)
	}
	if got := attrS(t, av, "Type"); got != "MESSAGE" {
		t.Errorf("Type = %q, want the const value MESSAGE", got)
	}
	if got, want := attrS(t, av, "GSI1_PK"), "T:"+item.TenantID.String()+"#MSG"; got != want {
		t.Errorf("GSI1_PK = %q, want %q", got, want)
	}
	if got := attrS(t, av, "GSI1_SK"); got != "M:01020304" {
		t.Errorf("GSI1_SK = %q, want M:01020304", got)
	}
}

// Writing the same item twice must produce the same attributes: a derived value is
// computed from its source fields, never from what the previous write left behind.
func TestDerivedAttributesAreIdempotent(t *testing.T) {
	table, _ := newDerivedTable(t)
	item := newDerivedItem()

	first, err := table.marshalItem(item)
	if err != nil {
		t.Fatalf("marshalItem: %v", err)
	}
	second, err := table.marshalItem(item)
	if err != nil {
		t.Fatalf("marshalItem: %v", err)
	}

	for _, name := range []string{"Type", "GSI1_PK", "GSI1_SK"} {
		if got, want := attrS(t, second, name), attrS(t, first, name); got != want {
			t.Errorf("%s drifted on the second write: %q, want %q", name, got, want)
		}
	}
}

// Recomputing a derived attribute does not by itself write it: an update writes the
// attributes the caller named, and nothing else.
func TestUpdateWritesDerivedAttributeOnlyWhenNamed(t *testing.T) {
	tests := []struct {
		name    string
		fields  []string
		wantSet []string
		wantOut []string // attributes that must stay out of the SET clause
	}{
		{
			name:    "not named",
			fields:  []string{"Title"},
			wantSet: []string{"Title"},
			wantOut: []string{"GSI1_SK", "Type"},
		},
		{
			name:    "named",
			fields:  []string{"Title", "GSI1_SK"},
			wantSet: []string{"Title", "GSI1_SK"},
			wantOut: []string{"Type"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			table, stub := newDerivedTable(t, okResponse(`{}`))
			item := newDerivedItem()
			item.GSI1SK = "stale"

			if err := table.Update(context.Background(), item, tc.fields...); err != nil {
				t.Fatalf("Update: %v", err)
			}

			req := stub.request(t, 0)
			expr := str(t, req, "UpdateExpression")
			names := sub(t, req, "ExpressionAttributeNames")
			setClause, _ := splitUpdateExpression(t, expr)

			for _, want := range tc.wantSet {
				if !clauseHasAttribute(setClause, names, want) {
					t.Errorf("SET clause %q does not write %s (names %v)", setClause, want, names)
				}
			}
			for _, unwanted := range tc.wantOut {
				if clauseHasAttribute(setClause, names, unwanted) {
					t.Errorf("SET clause %q writes %s, which was not named", setClause, unwanted)
				}
			}
			// Whatever is written is the recomputed value, not the stale one.
			if item.GSI1SK != "M:01020304" {
				t.Errorf("GSI1SK = %q, want the recomputed M:01020304", item.GSI1SK)
			}
		})
	}
}

// A derived attribute reaches DynamoDB through the real Put path, not just marshalItem.
func TestPutSendsDerivedAttributes(t *testing.T) {
	table, stub := newDerivedTable(t, okResponse(`{}`))
	if err := table.Put(context.Background(), newDerivedItem()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	item := sub(t, stub.request(t, 0), "Item")
	if got := str(t, sub(t, item, "Type"), "S"); got != "MESSAGE" {
		t.Errorf("Type = %q, want MESSAGE", got)
	}
	if got := str(t, sub(t, item, "GSI1_SK"), "S"); got != "M:01020304" {
		t.Errorf("GSI1_SK = %q, want M:01020304", got)
	}
}

// A derived attribute is stored and read back like any other attribute — the Table
// never splits it apart again, so a source field only has to be encodable.
type encodeOnlySourceItem struct {
	Tenant  string     `dynamo:"pk,prefix=T:"      dynamodbav:"-"`
	ID      string     `dynamo:"sk"                dynamodbav:"-"`
	Shard   encodeOnly `dynamodbav:"-"`
	ShardPK string     `dynamo:"format=S:{Shard}"  dynamodbav:"ShardPK"`
}

type nonStringDerivedItem struct {
	Tenant string `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string `dynamo:"sk"            dynamodbav:"-"`
	Kind   int    `dynamo:"const=42"      dynamodbav:"Kind"`
}

type unpersistedDerivedItem struct {
	Tenant string `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string `dynamo:"sk"            dynamodbav:"-"`
	Kind   string `dynamo:"const=META"    dynamodbav:"-"`
}

type selfPrefixDerivedItem struct {
	Tenant string `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string `dynamo:"sk"            dynamodbav:"-"`
	Label  string `dynamo:"prefix=L:"     dynamodbav:"Label"`
}

type selfFormatDerivedItem struct {
	Tenant string `dynamo:"pk,prefix=T:"    dynamodbav:"-"`
	ID     string `dynamo:"sk"              dynamodbav:"-"`
	Label  string `dynamo:"format=L:{Label}" dynamodbav:"Label"`
}

type selfBraceDerivedItem struct {
	Tenant string `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string `dynamo:"sk"            dynamodbav:"-"`
	Label  string `dynamo:"format=L:{}"   dynamodbav:"Label"`
}

type intSourceDerivedItem struct {
	Tenant  string `dynamo:"pk,prefix=T:"     dynamodbav:"-"`
	ID      string `dynamo:"sk"               dynamodbav:"-"`
	Shard   int    `dynamodbav:"Shard"`
	ShardPK string `dynamo:"format=S:{Shard}" dynamodbav:"ShardPK"`
}

type unknownSourceDerivedItem struct {
	Tenant  string `dynamo:"pk,prefix=T:"       dynamodbav:"-"`
	ID      string `dynamo:"sk"                 dynamodbav:"-"`
	ShardPK string `dynamo:"format=S:{Missing}" dynamodbav:"ShardPK"`
}

// New rejects a derived attribute it could not compute, or could only compute into a
// value that grows on every write.
func TestNewValidatesDerivedFields(t *testing.T) {
	tests := []struct {
		name    string
		build   func() error
		wantErr string // substring the error must mention; "" means it must succeed
	}{
		{"derived keys and discriminator", func() error { _, err := New[derivedItem](nil, "t"); return err }, ""},
		{"encode-only source", func() error { _, err := New[encodeOnlySourceItem](nil, "t"); return err }, ""},

		{"non-string target", func() error { _, err := New[nonStringDerivedItem](nil, "t"); return err }, "require a string field"},
		{"target is not persisted", func() error { _, err := New[unpersistedDerivedItem](nil, "t"); return err }, "need a persisted attribute"},
		{"prefix= on a non-key field", func() error { _, err := New[selfPrefixDerivedItem](nil, "t"); return err }, "cannot reference its own field"},
		{"format= referencing itself", func() error { _, err := New[selfFormatDerivedItem](nil, "t"); return err }, "cannot reference its own field"},
		{"bare {} on a non-key field", func() error { _, err := New[selfBraceDerivedItem](nil, "t"); return err }, "cannot reference its own field"},
		{"int source field", func() error { _, err := New[intSourceDerivedItem](nil, "t"); return err }, "unsupported source field type"},
		{"unknown source field", func() error { _, err := New[unknownSourceDerivedItem](nil, "t"); return err }, "unknown field"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected New to reject the derived field")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// The typo that motivated rejecting unknown options: "format:" instead of "format=".
// Ignoring it used to leave the pk silently encoded as a bare field value.
type typoTagItem struct {
	TenantID uuid.UUID `dynamo:"pk,format:T:{TenantID}#U:{UserID}" dynamodbav:"-"`
	UserID   uuid.UUID `dynamodbav:"-"`
	ID       string    `dynamo:"sk"                               dynamodbav:"-"`
}

type unknownOptionItem struct {
	Tenant string `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string `dynamo:"sk,ttl"        dynamodbav:"-"`
}

type trailingCommaItem struct {
	Tenant string `dynamo:"pk,prefix=T:," dynamodbav:"-"`
	ID     string `dynamo:"sk,"           dynamodbav:"-"`
}

func TestNewRejectsUnknownTagOptions(t *testing.T) {
	tests := []struct {
		name     string
		build    func() error
		wantErr  string
		wantOpts string // the offending option the error must quote
	}{
		{"format with a colon", func() error { _, err := New[typoTagItem](nil, "t"); return err }, "unknown dynamo tag option", "format:T:{TenantID}#U:{UserID}"},
		{"unsupported option", func() error { _, err := New[unknownOptionItem](nil, "t"); return err }, "unknown dynamo tag option", "ttl"},
		{"trailing comma is tolerated", func() error { _, err := New[trailingCommaItem](nil, "t"); return err }, "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected New to reject the unknown tag option")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantOpts) {
				t.Errorf("error = %v, want it to quote the option %q", err, tc.wantOpts)
			}
		})
	}
}

// A trailing comma must not change how the key is built.
func TestTrailingCommaKeepsThePrefix(t *testing.T) {
	table, err := New[trailingCommaItem](nil, "t")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	key, err := table.buildKey(&trailingCommaItem{Tenant: "acme", ID: "1"})
	if err != nil {
		t.Fatalf("buildKey: %v", err)
	}
	if got := attrS(t, key, "PK"); got != "T:acme" {
		t.Errorf("PK = %q, want T:acme", got)
	}
}

// A derived attribute is written by the transactional path too, which shares
// marshalItem with Put.
func TestPutTxWritesDerivedAttributes(t *testing.T) {
	table, _ := newDerivedTable(t)
	tx, err := table.PutTx(newDerivedItem(), nil)
	if err != nil {
		t.Fatalf("PutTx: %v", err)
	}
	if got := attrS(t, tx.Put.Item, "Type"); got != "MESSAGE" {
		t.Errorf("Type = %q, want MESSAGE", got)
	}
}
