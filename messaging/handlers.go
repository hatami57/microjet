package messaging

import (
	"context"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/jsonx"
	"github.com/hatami57/microjet/core/types"
)

// TypedHandler processes a message payload already decoded into T. Pair it with
// HandleJSON to get a raw Handler the broker can dispatch.
type TypedHandler[T any] func(ctx context.Context, payload T) error

// HandleJSON adapts a TypedHandler[T] into a raw Handler by JSON-decoding the
// message data into T before calling fn. A decode failure is returned as a
// BadRequest *errorx.Error (so it is distinguishable from a handler-side failure)
// and fn is not invoked.
//
//	sub, _ := client.Subscribe(ctx, "orders.created",
//	    messaging.HandleJSON(func(ctx context.Context, o Order) error { ... }))
func HandleJSON[T any](fn TypedHandler[T]) Handler {
	return func(ctx context.Context, msg *Message) error {
		var payload T
		if err := jsonx.FromJSON(string(msg.Data), &payload); err != nil {
			return errorx.NewBadRequestError("messaging", "decoding message payload failed").
				WithParams("subject", msg.Subject).WithInner(err)
		}
		return fn(ctx, payload)
	}
}

// EnvelopeHandler processes a decoded types.Message envelope together with its
// body extracted into T. Pair it with HandleEnvelope.
type EnvelopeHandler[T any] func(ctx context.Context, env *types.Message, body T) error

// HandleEnvelope adapts an EnvelopeHandler[T] into a raw Handler for messages
// carrying a types.Message JSON envelope: it decodes the envelope, extracts its
// body into T, and calls fn with both. Decode/extract failures are returned as
// BadRequest *errorx.Error values and fn is not invoked.
func HandleEnvelope[T any](fn EnvelopeHandler[T]) Handler {
	return func(ctx context.Context, msg *Message) error {
		var env types.Message
		if err := jsonx.FromJSON(string(msg.Data), &env); err != nil {
			return errorx.NewBadRequestError("messaging", "decoding message envelope failed").
				WithParams("subject", msg.Subject).WithInner(err)
		}
		var body T
		if err := env.ExtractBodyTo(&body); err != nil {
			return errorx.NewBadRequestError("messaging", "extracting envelope body failed").
				WithParams("subject", msg.Subject, "type", env.Type).WithInner(err)
		}
		return fn(ctx, &env, body)
	}
}

// NewJSONMessage builds a Message whose data is the JSON encoding of payload,
// for publishing a typed value without hand-marshaling. Headers are left nil;
// the broker client adds trace/correlation headers at publish time.
func NewJSONMessage[T any](subject string, payload T) (Message, error) {
	data, err := jsonx.ToJSON(payload)
	if err != nil {
		return Message{}, errorx.NewInternalError("messaging", "encoding message payload failed").
			WithParams("subject", subject).WithInner(err)
	}
	return Message{Subject: subject, Data: []byte(data)}, nil
}
