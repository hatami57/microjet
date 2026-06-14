package testx

import (
	"context"
	"sync"

	"github.com/hatami57/microjet/messaging"
)

// Broker is an in-memory messaging.Client for tests. Publish dispatches
// synchronously to every handler subscribed to the subject and records the
// message; Request routes to a registered responder. It is intentionally simple:
// queue groups are not load-balanced (every subscriber receives the message),
// which is sufficient for verifying handler wiring without a real broker.
type Broker struct {
	mu         sync.Mutex
	subs       map[string][]*subscription
	responders map[string]messaging.RequestHandler
	published  []messaging.Message
}

// NewBroker returns an empty in-memory broker.
func NewBroker() *Broker {
	return &Broker{subs: map[string][]*subscription{}, responders: map[string]messaging.RequestHandler{}}
}

var _ messaging.Client = (*Broker)(nil)

// Published returns a copy of every message passed to Publish, in order.
func (b *Broker) Published() []messaging.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]messaging.Message(nil), b.published...)
}

// Publish records msg and delivers it to all subscribers of its subject. A
// handler error is ignored here (the real clients log it); subscribe-side tests
// assert via their own captured state.
func (b *Broker) Publish(ctx context.Context, msg messaging.Message) error {
	b.mu.Lock()
	b.published = append(b.published, msg)
	handlers := append([]*subscription(nil), b.subs[msg.Subject]...)
	b.mu.Unlock()

	for _, s := range handlers {
		m := msg
		_ = s.handler(ctx, &m)
	}
	return nil
}

// Subscribe registers handler for subject.
func (b *Broker) Subscribe(_ context.Context, subject string, handler messaging.Handler) (messaging.Subscription, error) {
	return b.add(subject, handler), nil
}

// QueueSubscribe registers handler for subject. The queue group is recorded but
// not load-balanced; every subscriber receives each message.
func (b *Broker) QueueSubscribe(_ context.Context, subject, _ string, handler messaging.Handler) (messaging.Subscription, error) {
	return b.add(subject, handler), nil
}

func (b *Broker) add(subject string, handler messaging.Handler) *subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := &subscription{broker: b, subject: subject, handler: handler}
	b.subs[subject] = append(b.subs[subject], s)
	return s
}

// Request routes req to the responder registered for its subject via Respond,
// returning messaging.ErrTimeout when none is registered.
func (b *Broker) Request(ctx context.Context, req messaging.Request) (*messaging.Response, error) {
	b.mu.Lock()
	responder := b.responders[req.Subject]
	b.mu.Unlock()
	if responder == nil {
		return nil, messaging.ErrTimeout
	}
	return responder(ctx, &req)
}

// Respond registers handler for request-reply on command.
func (b *Broker) Respond(command string, handler messaging.RequestHandler) (messaging.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.responders[command] = handler
	return &subscription{broker: b, subject: command, responder: true}, nil
}

// QueueRespond behaves like Respond; the queue group is ignored.
func (b *Broker) QueueRespond(command, _ string, handler messaging.RequestHandler) (messaging.Subscription, error) {
	return b.Respond(command, handler)
}

// Healthy always reports healthy.
func (b *Broker) Healthy(context.Context) error { return nil }

// Connect, Disconnect, and IsConnected satisfy messaging.Client; the in-memory
// broker is always connected.
func (b *Broker) Connect() error    { return nil }
func (b *Broker) Disconnect() error { return nil }
func (b *Broker) IsConnected() bool { return true }

type subscription struct {
	broker    *Broker
	subject   string
	handler   messaging.Handler
	responder bool
}

func (s *subscription) Subject() string { return s.subject }

// Unsubscribe removes the subscription from its broker.
func (s *subscription) Unsubscribe() error {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	if s.responder {
		delete(s.broker.responders, s.subject)
		return nil
	}
	subs := s.broker.subs[s.subject]
	for i, existing := range subs {
		if existing == s {
			s.broker.subs[s.subject] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	return nil
}
