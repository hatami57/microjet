package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit rejects requests whose body exceeds maxBytes with 413 Request Entity
// Too Large. It is opt-in, like CORS; a non-positive maxBytes disables it.
//
// Two layers enforce the limit. A declared Content-Length over the limit is
// rejected up front with a clean 413 before the handler runs. For chunked or
// dishonestly-declared bodies the request body is wrapped in
// http.MaxBytesReader, which caps the bytes a handler can actually read: reading
// past the limit returns an error the handler surfaces (typically as 400/500,
// since errorx has no 413 category), and the wrapped writer suppresses further
// output. Set maxBytes to the largest payload any route legitimately accepts.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes <= 0 {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxBytes {
			c.Header("Connection", "close")
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":   "request_entity_too_large",
				"message": "request body exceeds the configured limit",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
