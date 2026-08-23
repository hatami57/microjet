package dynamo

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// code stands in for ulid.ULID: a value type whose canonical form is text, with
// MarshalText on the value and UnmarshalText on the pointer.
type code [4]byte

func (c code) MarshalText() ([]byte, error) { return []byte(hex.EncodeToString(c[:])), nil }

func (c *code) UnmarshalText(b []byte) error {
	decoded, err := hex.DecodeString(string(b))
	if err != nil {
		return err
	}
	if len(decoded) != len(c) {
		return fmt.Errorf("code: want %d bytes, got %d", len(c), len(decoded))
	}
	copy(c[:], decoded)
	return nil
}

// ptrCode implements both halves on the pointer, which is legal and has to work
// on the encode path too.
type ptrCode struct{ v string }

func (p *ptrCode) MarshalText() ([]byte, error) { return []byte(p.v), nil }
func (p *ptrCode) UnmarshalText(b []byte) error { p.v = string(b); return nil }

// encodeOnly marshals to text but cannot be parsed back: writing it would look
// fine and every read would fail, so it must be rejected up front.
type encodeOnly struct{ v string }

func (e encodeOnly) MarshalText() ([]byte, error) { return []byte(e.v), nil }

// namedString is a string type without the text interfaces.
type namedString string

// A key field must survive the encode/decode round trip, whichever standard
// interface it implements.
func TestKeyFieldRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "raw-value", "raw-value"},
		{"uuid", uuid.MustParse("6f0e3f1e-6a8f-4f2e-9a3d-2f5c9b1d7e21"), "6f0e3f1e-6a8f-4f2e-9a3d-2f5c9b1d7e21"},
		{"text marshaler value receiver", code{0xde, 0xad, 0xbe, 0xef}, "deadbeef"},
		{"text marshaler pointer receiver", ptrCode{v: "01HZY"}, "01HZY"},
		{"netip addr", netip.MustParseAddr("2001:db8::1"), "2001:db8::1"},
		{"time", time.Date(2026, 8, 23, 10, 30, 0, 0, time.UTC), "2026-08-23T10:30:00Z"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Fields are reached through a pointer's Elem, so mirror that here.
			src := reflect.New(reflect.TypeOf(tc.value)).Elem()
			src.Set(reflect.ValueOf(tc.value))

			got, err := fieldToString(src)
			if err != nil {
				t.Fatalf("fieldToString: %v", err)
			}
			if got != tc.want {
				t.Fatalf("fieldToString = %q, want %q", got, tc.want)
			}

			dst := reflect.New(reflect.TypeOf(tc.value)).Elem()
			if err := setFieldFromString(dst, got); err != nil {
				t.Fatalf("setFieldFromString: %v", err)
			}
			if !reflect.DeepEqual(dst.Interface(), tc.value) {
				t.Errorf("round trip = %#v, want %#v", dst.Interface(), tc.value)
			}
		})
	}
}

func TestSetFieldFromStringRejectsUnsupportedType(t *testing.T) {
	dst := reflect.New(reflect.TypeOf(0)).Elem()
	err := setFieldFromString(dst, "42")
	if err == nil {
		t.Fatal("expected an error for an int key field")
	}
	if !strings.Contains(err.Error(), "unsupported key field type") {
		t.Errorf("error = %v", err)
	}
}

type stringKeyItem struct {
	Tenant string `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string `dynamo:"sk,prefix=I:"  dynamodbav:"-"`
}

type codeKeyItem struct {
	Tenant string `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     code   `dynamo:"sk,prefix=C:"  dynamodbav:"-"`
}

type addrKeyItem struct {
	Addr netip.Addr `dynamo:"pk,prefix=A:"  dynamodbav:"-"`
	ID   string     `dynamo:"sk"            dynamodbav:"-"`
}

type intKeyItem struct {
	Tenant int    `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string `dynamo:"sk"            dynamodbav:"-"`
}

type intFormatItem struct {
	Tenant string `dynamo:"pk,format=T:{Tenant}#S:{Shard}" dynamodbav:"-"`
	Shard  int    `dynamodbav:"Shard"`
	ID     string `dynamo:"sk"                             dynamodbav:"-"`
}

type encodeOnlyKeyItem struct {
	Tenant encodeOnly `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string     `dynamo:"sk"            dynamodbav:"-"`
}

type namedStringKeyItem struct {
	Tenant namedString `dynamo:"pk,prefix=T:"  dynamodbav:"-"`
	ID     string      `dynamo:"sk"            dynamodbav:"-"`
}

// A const= key never encodes its field, so the field's type does not matter.
type constKeyItem struct {
	Tenant string `dynamo:"pk,prefix=T:"   dynamodbav:"-"`
	Kind   int    `dynamo:"sk,const=META"  dynamodbav:"-"`
}

// New validates tags eagerly, so an unusable key type is a startup error instead
// of a write that succeeds and a read that always fails.
func TestNewValidatesKeyFieldTypes(t *testing.T) {
	tests := []struct {
		name    string
		build   func() error
		wantErr string // substring the error must mention; "" means it must succeed
	}{
		{"string keys", func() error { _, err := New[stringKeyItem](nil, "t"); return err }, ""},
		{"uuid keys", func() error { _, err := New[testItem](nil, "t"); return err }, ""},
		{"text marshaler key", func() error { _, err := New[codeKeyItem](nil, "t"); return err }, ""},
		{"netip addr key", func() error { _, err := New[addrKeyItem](nil, "t"); return err }, ""},
		{"const key ignores the field type", func() error { _, err := New[constKeyItem](nil, "t"); return err }, ""},

		{"int key", func() error { _, err := New[intKeyItem](nil, "t"); return err }, "Tenant"},
		{"int field referenced by a format", func() error { _, err := New[intFormatItem](nil, "t"); return err }, "Shard"},
		{"encode-only key", func() error { _, err := New[encodeOnlyKeyItem](nil, "t"); return err }, "Tenant"},
		{"named string without text methods", func() error { _, err := New[namedStringKeyItem](nil, "t"); return err }, "Tenant"},
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
				t.Fatal("expected New to reject the unsupported key field type")
			}
			if !strings.Contains(err.Error(), "unsupported key field type") {
				t.Errorf("error = %v, want it to name the problem", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to name the field %q", err, tc.wantErr)
			}
		})
	}
}

// A key built from a TextMarshaler field has to survive the full encode/decode
// path, not just the string conversion.
func TestTableKeyRoundTripWithTextKey(t *testing.T) {
	table, err := New[codeKeyItem](nil, "t")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	item := &codeKeyItem{Tenant: "acme", ID: code{0x01, 0x02, 0x03, 0x04}}

	key, err := table.buildKey(item)
	if err != nil {
		t.Fatalf("buildKey: %v", err)
	}
	if got := attrS(t, key, "SK"); got != "C:01020304" {
		t.Errorf("SK = %q, want C:01020304", got)
	}

	var decoded codeKeyItem
	if err := table.injectKeys(&decoded, key); err != nil {
		t.Fatalf("injectKeys: %v", err)
	}
	if decoded != *item {
		t.Errorf("round trip = %+v, want %+v", decoded, *item)
	}
}

// format= composes a key from several fields and decodes it back by splitting on
// the literal separators.
func TestKeyFormatComposite(t *testing.T) {
	table, err := New[testItem](nil, "t")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	item := newTestItem()

	key, err := table.buildKey(item)
	if err != nil {
		t.Fatalf("buildKey: %v", err)
	}
	want := "T:" + item.TenantID.String() + "#U:" + item.UserID.String()
	if got := attrS(t, key, "PK"); got != want {
		t.Errorf("PK = %q, want %q", got, want)
	}

	var decoded testItem
	if err := table.injectKeys(&decoded, key); err != nil {
		t.Fatalf("injectKeys: %v", err)
	}
	if decoded.TenantID != item.TenantID || decoded.UserID != item.UserID || decoded.ID != item.ID {
		t.Errorf("decoded %v/%v/%v, want %v/%v/%v",
			decoded.TenantID, decoded.UserID, decoded.ID, item.TenantID, item.UserID, item.ID)
	}
}

type adjacentPlaceholderItem struct {
	Tenant string `dynamo:"pk,format={Tenant}{User}"  dynamodbav:"-"`
	User   string `dynamodbav:"User"`
	ID     string `dynamo:"sk"                        dynamodbav:"-"`
}

// Two placeholders with no literal between them encode fine but cannot be split
// apart again — the documented constraint.
func TestAdjacentPlaceholdersDoNotDecode(t *testing.T) {
	table, err := New[adjacentPlaceholderItem](nil, "t")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	item := &adjacentPlaceholderItem{Tenant: "acme", User: "bob", ID: "1"}

	key, err := table.buildKey(item)
	if err != nil {
		t.Fatalf("buildKey: %v", err)
	}
	if got := attrS(t, key, "PK"); got != "acmebob" {
		t.Errorf("PK = %q, want acmebob", got)
	}

	var decoded adjacentPlaceholderItem
	if err := table.injectKeys(&decoded, key); err != nil {
		t.Fatalf("injectKeys: %v", err)
	}
	if decoded.Tenant == item.Tenant && decoded.User == item.User {
		t.Error("adjacent placeholders decoded correctly; the documented limitation no longer holds")
	}
}
