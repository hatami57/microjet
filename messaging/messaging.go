package messaging

import (
	"context"

	"github.com/hatami57/microjet/core"
)

// Handler processes a received message. The context is derived from the
// subscription context and is cancelled when the subscription ends.
type Handler func(ctx context.Context, msg *Message) error
type RequestHandler func(ctx context.Context, req *Request) (*Response, error)

// Publisher defines a generic interface for message publishing
type Publisher interface {
	// Publish sends a message to the specified subject
	Publish(ctx context.Context, msg Message) error
}

// Subscriber defines a generic interface for message subscription
type Subscriber interface {
	// Subscribe registers a handler for the specified subject
	Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error)
	// QueueSubscribe registers a handler for the specified subject and queue group
	QueueSubscribe(ctx context.Context, subject string, queue string, handler Handler) (Subscription, error)
}

// Requester defines a generic interface for request-reply pattern
type Requester interface {
	// Request sends a request and waits for a reply
	Request(ctx context.Context, req Request) (*Response, error)
}

// Responder defines a generic interface for handling request-reply pattern
type Responder interface {
	// Respond registers a handler for requests on the specified subject
	Respond(command string, handler RequestHandler) (Subscription, error)
	// QueueRespond registers a handler for requests on the specified subject and a queue group
	QueueRespond(command string, queue string, handler RequestHandler) (Subscription, error)
}

// Client is the messaging abstraction used by the host. Publish and Subscribe
// take a context for cancellation/deadlines, and messages carry headers so
// metadata (correlation ids, trace context) survives across the broker.
type Client interface {
	Publisher
	Subscriber
	Requester
	Responder
	core.HealthChecker

	Connect() error
	Disconnect() error
	IsConnected() bool
}

// Subscription represents an active subscription that can be cancelled.
type Subscription interface {
	// Unsubscribe cancels the subscription
	Unsubscribe() error

	// Subject returns the subject of the subscription
	Subject() string
}
