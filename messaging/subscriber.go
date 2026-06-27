package messaging

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hatami57/microjet/host"
)

// subscriberBinding is one registered subject→handler subscription.
type subscriberBinding struct {
	name    string
	subject string
	queue   string
	handler Handler
}

// consumerService owns every subscription registered via Subscribe and ties
// them to the app lifecycle: it subscribes during the start phase (after the
// broker is connected) and unsubscribes on shutdown. A single instance
// aggregates all bindings so they share one lifecycle and shutdown context.
type consumerService struct {
	bindings []subscriberBinding
	logger   *slog.Logger
	cancel   context.CancelFunc
	subs     []Subscription
}

// Start implements host.ServiceStarter. It subscribes every binding using a
// shutdown-scoped context that is cancelled in Close, so in-flight handlers see
// cancellation. Runs in the host start phase, after the messaging client is
// connected.
func (s *consumerService) Start(app *host.App) error {
	client, ok := Lookup(app)
	if !ok {
		return fmt.Errorf("subscriber: no messaging client configured; install messaging.Module")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	for _, b := range s.bindings {
		var (
			sub Subscription
			err error
		)
		if b.queue != "" {
			sub, err = client.QueueSubscribe(ctx, b.subject, b.queue, b.handler)
		} else {
			sub, err = client.Subscribe(ctx, b.subject, b.handler)
		}
		if err != nil {
			return fmt.Errorf("subscriber %q: subscribing to %q: %w", b.name, b.subject, err)
		}
		s.subs = append(s.subs, sub)
		s.logger.Info("subscribed", "subscriber", b.name, "subject", b.subject, "queue", b.queue)
	}
	return nil
}

// CloseOrder closes the subscriber early (as an edge): it unsubscribes and drains
// in-flight handlers before the broker and the backends those handlers use are
// torn down.
func (s *consumerService) CloseOrder() int { return host.CloseEdge }

// Close implements host.ServiceCloser. It cancels the shutdown context and
// unsubscribes each subscription. Unsubscribe errors are logged rather than
// returned: during shutdown the broker may already be draining (its own Close
// flushes and unsubscribes), so a failure here is not actionable.
func (s *consumerService) Close(_ *host.App) error {
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

// SubscriberOption configures a subscription registered with Subscriber.
type SubscriberOption func(*subscriberBinding)

// WithQueueGroup makes the subscription join a queue group, so each message is
// delivered to only one member of the group — use it to load-balance a subject
// across replicas of the same service.
func WithQueueGroup(queue string) SubscriberOption {
	return func(b *subscriberBinding) { b.queue = queue }
}

// Subscribe installs a message subscription tied to the app lifecycle: it
// subscribes once the broker is connected and the app enters its start phase,
// and unsubscribes on shutdown. Requires messaging.Module. handler may be a raw
// Handler or one built with HandleJSON / HandleEnvelope for typed payloads:
//
//	host.MustNew().WithModules(
//	    messaging.Module(nats.New()),
//	    messaging.Subscribe("orders", "orders.created",
//	        messaging.HandleJSON(func(ctx context.Context, o Order) error { ... }),
//	        messaging.WithQueueGroup("order-workers")),
//	)
//
// Several Subscribe modules share one underlying consumer, so they start and
// stop together.
func Subscribe(name, subject string, handler Handler, opts ...SubscriberOption) host.Module {
	return host.ModuleFunc(func(app *host.App) error {
		if handler == nil {
			return fmt.Errorf("subscriber %q: nil handler", name)
		}
		binding := subscriberBinding{name: name, subject: subject, handler: handler}
		for _, opt := range opts {
			opt(&binding)
		}

		svc, ok := host.ResolveService[*consumerService](app)
		if !ok {
			svc = &consumerService{logger: app.Logger}
			host.ProvideService(app, svc)
		}
		svc.bindings = append(svc.bindings, binding)
		return nil
	})
}
