package types

import "github.com/hatami57/microjet/utils"

type Message struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Source    string         `json:"source"`
	Target    *string        `json:"target"`
	Headers   map[string]any `json:"headers"`
	Body      any            `json:"body"`
	CreatedAt int64          `json:"createdAt"`
}

func (b *Message) ExtractBodyTo(v any) error {
	return utils.AnyToStruct(b.Body, v)
}
