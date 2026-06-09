package core

import "context"

// Initer is implemented by services that need to perform initialization after
// their config is loaded but do not require host-level DI. The host calls Init
// on each registered service that implements this
// interface (host.ServiceIniter, which carries *App, takes precedence).
type Initer interface {
	Init() error
}

// Starter is implemented by services that begin active work (serving, listening)
// only after every service has finished Init. Splitting Start from Init gives the
// host a window between "resources acquired" and "serving" in which setup work
// (migrations, route registration) can run. The host calls Start on each
// registered service implementing this interface (host.ServiceStarter, which
// carries *App, takes precedence).
type Starter interface {
	Start() error
}

// Closer is implemented by services that need to release resources on shutdown.
// The host calls Close on each registered service that implements
// this interface (host.ServiceCloser takes precedence when present).
type Closer interface {
	Close() error
}

// HealthChecker is implemented by services that can report whether they are
// ready to serve traffic. The host's /readyz probe consults every registered
// service implementing it, so databases, cache, messaging, and any user service
// that implements this interface are covered without per-type wiring. Healthy
// returns nil when ready and a self-describing error otherwise.
type HealthChecker interface {
	Healthy(ctx context.Context) error
}
