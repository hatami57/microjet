package messaging

import "errors"

// ErrTimeout is returned by Requester.Request when a reply is not received in
// time. Transports normalise their broker-specific timeout errors into this
// sentinel so callers can detect timeouts with errors.Is without importing a
// broker package.
var ErrTimeout = errors.New("messaging: request timed out")
