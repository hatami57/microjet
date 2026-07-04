// Package core provides foundational primitives shared across MicroJet: an
// injectable time/clock abstraction, correlation-ID propagation, and the
// service lifecycle interfaces (Initer, Starter, Closer, HealthChecker).
package core

import "context"

// Initer is implemented by services that need to perform initialization after
// their config is loaded but do not require host-level DI. The host calls Init
// on each registered service that implements this
// interface (host.ServiceIniter, which carries *App, takes precedence).
type Initer interface {
	Init() error
}

// Setupper is implemented by services that need to perform post-init work once
// every service has finished Init — typically work that depends on other
// services being connected (running migrations, finalizing route registration).
// It runs in the same phase as host.App.Setup handlers but is co-located on the
// service itself. The host calls Setup on each registered service implementing
// this interface (host.ServiceSetupper, which carries *App, takes precedence).
type Setupper interface {
	Setup() error
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

// ReadinessToggler is implemented by services whose readiness can be flipped —
// typically an HTTP server backing /readyz. At the start of graceful shutdown the
// host flips every such service to not-ready and waits app.shutdownDelay before
// tearing anything down, so Kubernetes drops the pod from its endpoints (and load
// balancers stop routing new requests) before in-flight work is drained. Liveness
// (/health) must stay healthy throughout, or the kubelet restarts the pod
// mid-drain.
type ReadinessToggler interface {
	SetReady(ready bool)
}

// HealthChecker is implemented by services that can report whether they are
// ready to serve traffic. The host's /readyz probe consults every registered
// service implementing it, so databases, cache, messaging, and any user service
// that implements this interface are covered without per-type wiring. Healthy
// returns nil when ready and a self-describing error otherwise.
type HealthChecker interface {
	Healthy(ctx context.Context) error
}
