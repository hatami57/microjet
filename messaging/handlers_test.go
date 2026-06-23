package messaging

import (
	"context"
	"testing"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/jsonx"
)

type order struct {
	ID    string `json:"id"`
	Total int    `json:"total"`
}

func TestHandleJSONDecodesPayload(t *testing.T) {
	var got order
	h := HandleJSON(func(_ context.Context, o order) error {
		got = o
		return nil
	})
	err := h(context.Background(), &Message{Subject: "orders", Data: []byte(`{"id":"a","total":5}`)})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got.ID != "a" || got.Total != 5 {
		t.Errorf("decoded = %+v", got)
	}
}

func TestHandleJSONBadPayloadReturnsBadRequest(t *testing.T) {
	called := false
	h := HandleJSON(func(_ context.Context, _ order) error {
		called = true
		return nil
	})
	err := h(context.Background(), &Message{Subject: "orders", Data: []byte(`not json`)})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errorx.IsBadRequestError(err) {
		t.Errorf("error = %v, want BadRequest", err)
	}
	if called {
		t.Error("handler should not run on decode failure")
	}
}

func TestHandleEnvelopeExtractsBody(t *testing.T) {
	env := Envelope{Type: "order.created", ID: "evt-1", Body: order{ID: "b", Total: 9}}
	encoded, err := jsonx.ToJSON(env)
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	data := []byte(encoded)

	var gotEnv *Envelope
	var gotBody order
	h := HandleEnvelope(func(_ context.Context, e *Envelope, o order) error {
		gotEnv, gotBody = e, o
		return nil
	})
	if err := h(context.Background(), &Message{Subject: "orders", Data: data}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if gotEnv == nil || gotEnv.ID != "evt-1" || gotEnv.Type != "order.created" {
		t.Errorf("envelope = %+v", gotEnv)
	}
	if gotBody.ID != "b" || gotBody.Total != 9 {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestNewJSONMessageRoundTrips(t *testing.T) {
	msg, err := NewJSONMessage("orders.created", order{ID: "c", Total: 3})
	if err != nil {
		t.Fatalf("NewJSONMessage: %v", err)
	}
	if msg.Subject != "orders.created" {
		t.Errorf("subject = %q", msg.Subject)
	}
	var got order
	if err := HandleJSON(func(_ context.Context, o order) error {
		got = o
		return nil
	})(context.Background(), &msg); err != nil {
		t.Fatalf("decode round-trip: %v", err)
	}
	if got != (order{ID: "c", Total: 3}) {
		t.Errorf("round-trip = %+v", got)
	}
}
