package messaging

import (
	"context"
	"sync"
	"testing"

	"github.com/hatami57/microjet/host"
)

// fakeBroker is a minimal in-memory Client for lifecycle tests. It records
// subscriptions and can deliver a message to the handler registered for a
// subject.
type fakeBroker struct {
	mu           sync.Mutex
	handlers     map[string]Handler
	queues       map[string]string
	unsubscribed []string
	published    []Message
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{handlers: map[string]Handler{}, queues: map[string]string{}}
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

func (b *fakeBroker) Subscribe(_ context.Context, subject string, h Handler) (Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[subject] = h
	return &fakeSub{broker: b, subject: subject}, nil
}

func (b *fakeBroker) QueueSubscribe(_ context.Context, subject, queue string, h Handler) (Subscription, error) {
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
	if err := h(context.Background(), &Message{Subject: subject, Data: data}); err != nil {
		t.Fatalf("handler for %q: %v", subject, err)
	}
}

func (b *fakeBroker) Publish(_ context.Context, msg Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, msg)
	return nil
}

// Unused interface methods.
func (b *fakeBroker) Request(context.Context, Request) (*Response, error)  { return nil, nil }
func (b *fakeBroker) Respond(string, RequestHandler) (Subscription, error) { return nil, nil }
func (b *fakeBroker) QueueRespond(string, string, RequestHandler) (Subscription, error) {
	return nil, nil
}
func (b *fakeBroker) Healthy(context.Context) error { return nil }
func (b *fakeBroker) Connect() error                { return nil }
func (b *fakeBroker) Disconnect() error             { return nil }
func (b *fakeBroker) IsConnected() bool             { return true }

func newAppWithBroker(t *testing.T, broker Client) *host.App {
	t.Helper()
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	app.WithModule(Module(broker))
	if app.Err() != nil {
		t.Fatalf("Module: %v", app.Err())
	}
	return app
}

func TestSubscribeDispatchesTypedPayload(t *testing.T) {
	broker := newFakeBroker()
	app := newAppWithBroker(t, broker)

	type evt struct {
		Name string `json:"name"`
	}
	var got string
	app.WithModule(Subscribe("greetings", "greet", HandleJSON(func(_ context.Context, e evt) error {
		got = e.Name
		return nil
	})))
	if app.Err() != nil {
		t.Fatalf("Subscribe: %v", app.Err())
	}

	svc, ok := host.ResolveService[*consumerService](app)
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

func TestSubscribeQueueGroup(t *testing.T) {
	broker := newFakeBroker()
	app := newAppWithBroker(t, broker)

	app.WithModule(Subscribe("workers", "jobs", func(context.Context, *Message) error { return nil },
		WithQueueGroup("pool")))

	svc, _ := host.ResolveService[*consumerService](app)
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

	noop := func(context.Context, *Message) error { return nil }
	app.WithModules(Subscribe("a", "subj.a", noop), Subscribe("b", "subj.b", noop))

	svc, ok := host.ResolveService[*consumerService](app)
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

func TestSubscribeNilHandlerFails(t *testing.T) {
	app := newAppWithBroker(t, newFakeBroker())
	app.WithModule(Subscribe("bad", "subj", nil))
	if app.Err() == nil {
		t.Error("expected a deferred error for a nil handler")
	}
}

func TestConsumerStartWithoutMessagingFails(t *testing.T) {
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	svc := &consumerService{logger: app.Logger}
	if err := svc.Start(app); err == nil {
		t.Error("expected an error when no messaging client is configured")
	}
}
