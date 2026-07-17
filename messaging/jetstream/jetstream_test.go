package jetstream

// The integration tests here need a real NATS server with JetStream enabled, so
// they are gated behind the MICROJET_TEST_NATS_URL environment variable and skip
// when it is unset — the same convention as the outbox Postgres test. This keeps
// the shipped module's dependency graph to the nats.go client alone; nothing
// links an embedded server. The pure-logic tests below always run.
//
// Run locally against a throwaway JetStream server, e.g.:
//
//	nats-server -js
//	MICROJET_TEST_NATS_URL='nats://localhost:4222' go test -race ./messaging/jetstream/
//
// Each test purges the EVENTS stream first, so point it at a server you do not
// mind clobbering.

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hatami57/microjet/messaging"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// requireNATS returns the broker URL from MICROJET_TEST_NATS_URL, skipping the
// test when it is unset.
func requireNATS(t *testing.T) string {
	t.Helper()
	url := os.Getenv("MICROJET_TEST_NATS_URL")
	if url == "" {
		t.Skip("set MICROJET_TEST_NATS_URL to a JetStream-enabled NATS server to run this test")
	}
	return url
}

// newTestClient purges the EVENTS stream for isolation, then connects a Client
// that (re)creates it over events.>.
func newTestClient(t *testing.T, url string, js JetStreamConfig) *Client {
	t.Helper()
	purgeStream(t, url, "EVENTS")
	if js.MaxAckPending == 0 {
		js.MaxAckPending = 100
	}
	js.Streams = append(js.Streams, StreamSpec{Name: "EVENTS", Subjects: []string{"events.>"}})
	c := New()
	c.Config = Config{URL: url, JetStream: js}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		purgeStream(t, url, "EVENTS")
	})
	return c
}

// purgeStream deletes the named stream if present, giving each test a clean slate
// on a persistent server.
func purgeStream(t *testing.T, url, name string) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("admin jetstream: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := js.DeleteStream(ctx, name); err != nil && err != jetstream.ErrStreamNotFound {
		t.Fatalf("delete stream %q: %v", name, err)
	}
}

func TestPublishThenSubscribeRoundTrip(t *testing.T) {
	c := newTestClient(t, requireNATS(t), JetStreamConfig{AckWait: time.Second, MaxDeliver: 5})

	got := make(chan *messaging.Message, 1)
	if _, err := c.Subscribe(context.Background(), "events.hello", func(_ context.Context, m *messaging.Message) error {
		got <- m
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := c.Publish(context.Background(), messaging.Message{Subject: "events.hello", Data: []byte("hi")}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case m := <-got:
		if string(m.Data) != "hi" {
			t.Fatalf("data = %q, want hi", m.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("message not delivered")
	}
}

// TestPublishedBeforeSubscribeIsDelivered is the JetStream value proposition: a
// message published while no consumer exists is retained and delivered once a
// durable consumer subscribes — the at-least-once guarantee core NATS lacks.
func TestPublishedBeforeSubscribeIsDelivered(t *testing.T) {
	c := newTestClient(t, requireNATS(t), JetStreamConfig{AckWait: time.Second, MaxDeliver: 5})

	if err := c.Publish(context.Background(), messaging.Message{Subject: "events.retained", Data: []byte("kept")}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := make(chan []byte, 1)
	if _, err := c.Subscribe(context.Background(), "events.retained", func(_ context.Context, m *messaging.Message) error {
		got <- m.Data
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	select {
	case data := <-got:
		if string(data) != "kept" {
			t.Fatalf("data = %q, want kept", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("message published before subscribe was not retained/delivered")
	}
}

func TestRedeliveryThenSuccess(t *testing.T) {
	c := newTestClient(t, requireNATS(t), JetStreamConfig{AckWait: 500 * time.Millisecond, MaxDeliver: 5})

	var attempts atomic.Int32
	done := make(chan struct{})
	if _, err := c.Subscribe(context.Background(), "events.retry", func(_ context.Context, _ *messaging.Message) error {
		if attempts.Add(1) < 3 {
			return context.DeadlineExceeded // fail the first two attempts
		}
		close(done)
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := c.Publish(context.Background(), messaging.Message{Subject: "events.retry", Data: []byte("x")}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case <-done:
		if n := attempts.Load(); n != 3 {
			t.Fatalf("attempts = %d, want 3 (two redeliveries then success)", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("handler never succeeded after redelivery; attempts=%d", attempts.Load())
	}
}

func TestDeadLetterAfterMaxDeliver(t *testing.T) {
	url := requireNATS(t)
	c := newTestClient(t, url, JetStreamConfig{AckWait: 500 * time.Millisecond, MaxDeliver: 2, DeadLetterSubject: "dlq.events"})

	// A raw core-NATS subscriber captures the dead-lettered copy.
	raw, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("raw connect: %v", err)
	}
	t.Cleanup(raw.Close)
	dlq := make(chan *nats.Msg, 1)
	if _, err := raw.ChanSubscribe("dlq.events", dlq); err != nil {
		t.Fatalf("dlq subscribe: %v", err)
	}

	if _, err := c.Subscribe(context.Background(), "events.poison", func(_ context.Context, _ *messaging.Message) error {
		return context.DeadlineExceeded // always fail
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := c.Publish(context.Background(), messaging.Message{Subject: "events.poison", Data: []byte("bad")}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case m := <-dlq:
		if string(m.Data) != "bad" {
			t.Fatalf("dlq data = %q, want bad", m.Data)
		}
		if orig := m.Header.Get("X-Original-Subject"); orig != "events.poison" {
			t.Fatalf("X-Original-Subject = %q, want events.poison", orig)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("message was not dead-lettered after exhausting MaxDeliver")
	}
}

func TestRequestRespondOverCoreNATS(t *testing.T) {
	c := newTestClient(t, requireNATS(t), JetStreamConfig{AckWait: time.Second, MaxDeliver: 5})

	if _, err := c.Respond("q.echo", func(_ context.Context, req *messaging.Request) (*messaging.Response, error) {
		return &messaging.Response{Data: append([]byte("echo:"), req.Data...)}, nil
	}); err != nil {
		t.Fatalf("respond: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := c.Request(ctx, messaging.Request{Subject: "q.echo", Data: []byte("ping")})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if string(resp.Data) != "echo:ping" {
		t.Fatalf("reply = %q, want echo:ping", resp.Data)
	}
}

func TestDurableNameSanitizes(t *testing.T) {
	c := &Client{Config: Config{JetStream: JetStreamConfig{DurablePrefix: "svc.a"}}}
	got := c.durableName("orders.created.*")
	want := "svc_a_orders_created__"
	if got != want {
		t.Fatalf("durableName = %q, want %q", got, want)
	}

	plain := (&Client{}).durableName("orders.created")
	if plain != "orders_created" {
		t.Fatalf("durableName without prefix = %q, want orders_created", plain)
	}
}

func TestRetentionAndStorageMapping(t *testing.T) {
	if retentionPolicy("workqueue") != jetstream.WorkQueuePolicy {
		t.Errorf("workqueue mapped to %v", retentionPolicy("workqueue"))
	}
	if retentionPolicy("interest") != jetstream.InterestPolicy {
		t.Errorf("interest mapped to %v", retentionPolicy("interest"))
	}
	if retentionPolicy("") != jetstream.LimitsPolicy {
		t.Errorf("empty retention should default to LimitsPolicy, got %v", retentionPolicy(""))
	}
	if storageType("memory") != jetstream.MemoryStorage {
		t.Errorf("memory storage mapping wrong: %v", storageType("memory"))
	}
	if storageType("") != jetstream.FileStorage {
		t.Errorf("empty storage should default to FileStorage, got %v", storageType(""))
	}
}
