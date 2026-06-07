package messaging

import "github.com/hatami57/microjet/core"

// CorrelationIDHeader is the header used to carry a correlation (request) id
// across the broker. It is the canonical correlation-id header defined in core,
// so a request id flows uniformly http -> messaging -> http.
const CorrelationIDHeader = core.CorrelationIDHeader

// HeaderValue returns the first value stored under key, or "" if the header is
// absent or empty.
func HeaderValue(headers map[string][]string, key string) string {
	if vs, ok := headers[key]; ok && len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// SetHeader returns headers with key set to a single value, allocating the map
// when it is nil. The (possibly new) map is returned so callers can write:
//
//	msg.Headers = messaging.SetHeader(msg.Headers, k, v)
func SetHeader(headers map[string][]string, key, value string) map[string][]string {
	if headers == nil {
		headers = make(map[string][]string, 1)
	}
	headers[key] = []string{value}
	return headers
}

// CorrelationID returns the correlation id carried in headers, or "" if none.
func CorrelationID(headers map[string][]string) string {
	return HeaderValue(headers, CorrelationIDHeader)
}

// SetCorrelationID returns headers with the correlation id set, allocating the
// map when it is nil.
func SetCorrelationID(headers map[string][]string, id string) map[string][]string {
	return SetHeader(headers, CorrelationIDHeader, id)
}
