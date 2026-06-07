package messaging

import "errors"

// ErrTimeout is returned when a request-reply call exceeds its deadline or
// the broker reports no responders.
var ErrTimeout = errors.New("messaging: timeout")
