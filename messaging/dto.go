package messaging

// Message is a published or received message. Headers carry metadata such as a
// correlation id (e.g. "X-Request-ID") so it can be propagated across services.
type Message struct {
	Subject string
	Data    []byte
	Headers map[string][]string
}

type Request struct {
	Subject string
	Data    []byte
	Headers map[string][]string
}

type Response struct {
	Subject string
	Data    []byte
	Reply   string
	Headers map[string][]string
}
