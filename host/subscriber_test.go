package host

import (
	"context"
	"sync"
	"testing"

	"github.com/hatami57/microjet/messaging"
)

// fakeBroker is a minimal in-memory messaging.Client for lifecycle tests. It
// records subscriptions and can deliver a message to the handler registered for
// a subject.
type fakeBroker struct {
	mu           sync.Mutex
	handlers     map[string]messaging.Handler
	queues       map[string]string
	unsubscribed []string
	published    []messaging.Message
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{handlers: map[string]messaging.Handler{}, queues: map[string]string{}}
}

type fakeSub struct {
	broker  *fakeBroker
	subject string
}

func (s *fakeSub) Unsubscribe() error {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	s.broker.unsubscribed = append(s.broker.unsubscribed, s.subject)
	return nil
}
func (s *fakeSub) Subject() string { return s.subject }

func (b *fakeBroker) Subscribe(_ context.Context, subject string, h messaging.Handler) (messaging.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[subject] = h
	return &fakeSub{broker: b, subject: subject}, nil
}

func (b *fakeBroker) QueueSubscribe(_ context.Context, subject, queue string, h messaging.Handler) (messaging.Subscription, error) {
	b.mu.Lock()
	b.queues[subject] = queue
	b.mu.Unlock()
	return b.Subscribe(context.Background(), subject, h)
}

func (b *fakeBroker) deliver(t *testing.T, subject string, data []byte) {
	t.Helper()
	b.mu.Lock()
	h := b.handlers[subject]
	b.mu.Unlock()
	if h == nil {
		t.Fatalf("no handler registered for %q", subject)
	}
	if err := h(context.Background(), &messaging.Message{Subject: subject, Data: data}); err != nil {
		t.Fatalf("handler for %q: %v", subject, err)
	}
}

func (b *fakeBroker) Publish(_ context.Context, msg messaging.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, msg)
	return nil
}

// Unused interface methods.
func (b *fakeBroker) Request(context.Context, messaging.Request) (*messaging.Response, error) {
	return nil, nil
}
func (b *fakeBroker) Respond(string, messaging.RequestHandler) (messaging.Subscription, error) {
	return nil, nil
}
func (b *fakeBroker) QueueRespond(string, string, messaging.RequestHandler) (messaging.Subscription, error) {
	return nil, nil
}
func (b *fakeBroker) Healthy(context.Context) error { return nil }
func (b *fakeBroker) Connect() error                { return nil }
func (b *fakeBroker) Disconnect() error             { return nil }
func (b *fakeBroker) IsConnected() bool             { return true }

func newAppWithBroker(t *testing.T, broker messaging.Client) *App {
	t.Helper()
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	app.WithMessaging(broker)
	if app.Err() != nil {
		t.Fatalf("WithMessaging: %v", app.Err())
	}
	return app
}

func TestWithSubscriberDispatchesTypedPayload(t *testing.T) {
	broker := newFakeBroker()
	app := newAppWithBroker(t, broker)

	type evt struct {
		Name string `json:"name"`
	}
	var got string
	app.WithSubscriber("greetings", "greet", messaging.HandleJSON(func(_ context.Context, e evt) error {
		got = e.Name
		return nil
	}))
	if app.Err() != nil {
		t.Fatalf("WithSubscriber: %v", app.Err())
	}

	svc, ok := ResolveService[*consumerService](app)
	if !ok {
		t.Fatal("consumer service not registered")
	}
	if err := svc.Start(app); err != nil {
		t.Fatalf("Start: %v", err)
	}

	broker.deliver(t, "greet", []byte(`{"name":"world"}`))
	if got != "world" {
		t.Errorf("handler payload = %q, want world", got)
	}

	if err := svc.Close(app); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(broker.unsubscribed) != 1 || broker.unsubscribed[0] != "greet" {
		t.Errorf("unsubscribed = %v, want [greet]", broker.unsubscribed)
	}
}

func TestWithSubscriberQueueGroup(t *testing.T) {
	broker := newFakeBroker()
	app := newAppWithBroker(t, broker)

	app.WithSubscriber("workers", "jobs", func(context.Context, *messaging.Message) error { return nil },
		WithQueueGroup("pool"))

	svc, _ := ResolveService[*consumerService](app)
	if err := svc.Start(app); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if broker.queues["jobs"] != "pool" {
		t.Errorf("queue group = %q, want pool", broker.queues["jobs"])
	}
}

func TestMultipleSubscribersShareOneConsumer(t *testing.T) {
	broker := newFakeBroker()
	app := newAppWithBroker(t, broker)

	noop := func(context.Context, *messaging.Message) error { return nil }
	app.WithSubscriber("a", "subj.a", noop).WithSubscriber("b", "subj.b", noop)

	svc, ok := ResolveService[*consumerService](app)
	if !ok {
		t.Fatal("consumer service not registered")
	}
	if len(svc.bindings) != 2 {
		t.Fatalf("bindings = %d, want 2", len(svc.bindings))
	}
	if err := svc.Start(app); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(broker.handlers) != 2 {
		t.Errorf("registered handlers = %d, want 2", len(broker.handlers))
	}
}

func TestWithSubscriberNilHandlerFails(t *testing.T) {
	app := newAppWithBroker(t, newFakeBroker())
	app.WithSubscriber("bad", "subj", nil)
	if app.Err() == nil {
		t.Error("expected a deferred error for a nil handler")
	}
}

func TestConsumerStartWithoutMessagingFails(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc := &consumerService{logger: app.Logger}
	if err := svc.Start(app); err == nil {
		t.Error("expected an error when no messaging client is configured")
	}
}
