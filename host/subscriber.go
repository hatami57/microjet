package host

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hatami57/microjet/messaging"
)

// subscriberBinding is one registered subject→handler subscription.
type subscriberBinding struct {
	name    string
	subject string
	queue   string
	handler messaging.Handler
}

// consumerService owns every subscription registered via WithSubscriber and
// ties them to the app lifecycle: it subscribes during the start phase (after
// the broker is connected) and unsubscribes on shutdown. A single instance
// aggregates all bindings so they share one lifecycle and shutdown context.
type consumerService struct {
	bindings []subscriberBinding
	logger   *slog.Logger
	cancel   context.CancelFunc
	subs     []messaging.Subscription
}

// Start implements ServiceStarter. It subscribes every binding using a
// shutdown-scoped context that is cancelled in Close, so in-flight handlers see
// cancellation. Runs in the host start phase, after services (including the
// messaging client) are initialized and connected.
func (s *consumerService) Start(app *App) error {
	if app.Messaging == nil {
		return fmt.Errorf("subscriber: no messaging client configured; call WithMessaging")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	for _, b := range s.bindings {
		var (
			sub messaging.Subscription
			err error
		)
		if b.queue != "" {
			sub, err = app.Messaging.QueueSubscribe(ctx, b.subject, b.queue, b.handler)
		} else {
			sub, err = app.Messaging.Subscribe(ctx, b.subject, b.handler)
		}
		if err != nil {
			return fmt.Errorf("subscriber %q: subscribing to %q: %w", b.name, b.subject, err)
		}
		s.subs = append(s.subs, sub)
		s.logger.Info("subscribed", "subscriber", b.name, "subject", b.subject, "queue", b.queue)
	}
	return nil
}

// Close implements ServiceCloser. It cancels the shutdown context and
// unsubscribes each subscription. Unsubscribe errors are logged rather than
// returned: during shutdown the broker may already be draining (its own Close
// flushes and unsubscribes), so a failure here is not actionable.
func (s *consumerService) Close(_ *App) error {
	if s.cancel != nil {
		s.cancel()
	}
	for _, sub := range s.subs {
		if err := sub.Unsubscribe(); err != nil {
			s.logger.Warn("unsubscribe failed", "subject", sub.Subject(), "error", err)
		}
	}
	return nil
}

// SubscriberOption configures a subscription registered with WithSubscriber.
type SubscriberOption func(*subscriberBinding)

// WithQueueGroup makes the subscription join a queue group, so each message is
// delivered to only one member of the group — use it to load-balance a subject
// across replicas of the same service.
func WithQueueGroup(queue string) SubscriberOption {
	return func(b *subscriberBinding) { b.queue = queue }
}

// WithSubscriber registers a message subscription tied to the app lifecycle: it
// subscribes once the broker is connected and the app enters its start phase,
// and unsubscribes on shutdown. Requires WithMessaging. handler may be a raw
// messaging.Handler or one built with messaging.HandleJSON / HandleEnvelope for
// typed payloads:
//
//	app.WithMessaging(nats.New()).
//	    WithSubscriber("orders", "orders.created",
//	        messaging.HandleJSON(func(ctx context.Context, o Order) error { ... }),
//	        host.WithQueueGroup("order-workers"))
//
// Errors are deferred to Run/MustRun/Err.
func (a *App) WithSubscriber(name, subject string, handler messaging.Handler, opts ...SubscriberOption) *App {
	if a.err != nil {
		return a
	}
	if handler == nil {
		return a.fail(fmt.Errorf("subscriber %q: nil handler", name))
	}
	binding := subscriberBinding{name: name, subject: subject, handler: handler}
	for _, opt := range opts {
		opt(&binding)
	}

	svc, ok := ResolveService[*consumerService](a)
	if !ok {
		svc = &consumerService{logger: a.Logger}
		ProvideService(a, svc)
	}
	svc.bindings = append(svc.bindings, binding)
	return a
}
