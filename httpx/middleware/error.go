package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core/errorx"
)

// Error translates errors recorded on the gin context into structured JSON
// responses. When debug is false, inner causes are never exposed to clients.
func Error(debug bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		err := c.Errors.Last().Err
		status := http.StatusInternalServerError

		var response errorx.ErrorResponse
		var v *errorx.Error
		switch {
		case errors.As(err, &v):
			var innerError *string
			if debug && v.Inner != nil {
				s := v.Inner.Error()
				innerError = &s
			}
			response = errorx.ErrorResponse{
				Subject:    v.Subject,
				Message:    v.Message,
				Params:     v.Params,
				Code:       v.Code,
				InnerError: innerError,
			}
			switch v.Type {
			case errorx.NotFoundErrorType:
				status = http.StatusNotFound
				response.Error = "not_found"
			case errorx.BadRequestErrorType:
				status = http.StatusBadRequest
				response.Error = "invalid_input"
			case errorx.BusinessErrorType:
				status = http.StatusConflict
				response.Error = "conflict"
			case errorx.UnauthorizedErrorType:
				status = http.StatusUnauthorized
				response.Error = "unauthorized"
			case errorx.ForbiddenErrorType:
				status = http.StatusForbidden
				response.Error = "forbidden"
			default:
				// Internal or unknown typed error.
				response.Error = "internal_server_error"
			}
		default:
			// Untyped error: never expose the raw string in production.
			response = errorx.ErrorResponse{
				Error:   "internal_server_error",
				Subject: "Unknown",
				Message: "An internal server error occurred",
			}
			if debug {
				s := err.Error()
				response.InnerError = &s
			}
		}

		c.JSON(status, response)
		c.Abort()
	}
}
