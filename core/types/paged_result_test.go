package types

import "testing"

type sampleCursor struct {
	LastID int
	Name   string
}

func TestPageTokenRoundTrip(t *testing.T) {
	token, err := EncodePageToken(sampleCursor{LastID: 7, Name: "alice"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if token == nil || *token == "" {
		t.Fatal("expected non-empty token")
	}

	decoded, err := DecodePageToken[sampleCursor](token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded == nil || decoded.LastID != 7 || decoded.Name != "alice" {
		t.Fatalf("decoded = %+v, want {7 alice}", decoded)
	}
}

func TestDecodeNilOrEmptyToken(t *testing.T) {
	if got, err := DecodePageToken[sampleCursor](nil); err != nil || got != nil {
		t.Errorf("nil token: got %v, %v; want nil, nil", got, err)
	}
	empty := ""
	if got, err := DecodePageToken[sampleCursor](&empty); err != nil || got != nil {
		t.Errorf("empty token: got %v, %v; want nil, nil", got, err)
	}
}

func TestDecodeInvalidToken(t *testing.T) {
	bad := "not-base64!!!"
	if _, err := DecodePageToken[sampleCursor](&bad); err == nil {
		t.Error("expected error for invalid base64 token")
	}
}
