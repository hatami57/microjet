package messaging

import "github.com/hatami57/microjet/core/jsonx"

// Envelope is a message envelope carrying metadata alongside an arbitrary Body.
// Use ExtractBodyTo to decode Body into a concrete type.
type Envelope struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Source    string         `json:"source"`
	Target    *string        `json:"target"`
	Headers   map[string]any `json:"headers"`
	Body      any            `json:"body"`
	CreatedAt int64          `json:"createdAt"`
}

func (b *Envelope) ExtractBodyTo(v any) error {
	return jsonx.AnyToStruct(b.Body, v)
}
