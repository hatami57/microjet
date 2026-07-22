package middleware

import (
	"errors"
	"log/slog"
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

		// Log the causes server-side. The response only renders the last error
		// and scrubs inner causes in production, so this is the only place the
		// underlying failures are captured — and a handler may attach several.
		logErrors(c)

		c.JSON(status, response)
		c.Abort()
	}
}

// logErrors writes every error accumulated on the gin context to the
// request-scoped logger. Each is logged on its own line at a level derived from
// its type — internal/untyped faults at Error, client-caused (4xx) errors at
// Warn. For a typed *errorx.Error the structured fields (subject, code, message,
// inner cause) are logged individually; err.Error() is not, because it already
// embeds those and would duplicate them.
func logErrors(c *gin.Context) {
	ctx := c.Request.Context()
	logger := LoggerFromContext(ctx)

	for _, ginErr := range c.Errors {
		err := ginErr.Err

		var v *errorx.Error
		if errors.As(err, &v) {
			attrs := []slog.Attr{
				slog.String("subject", v.Subject),
				slog.String("message", v.Message),
				slog.Int("code", v.Code),
			}
			if v.Inner != nil {
				attrs = append(attrs, slog.String("cause", v.Inner.Error()))
			}
			logger.LogAttrs(ctx, errorLevel(v), "request failed", attrs...)
		} else {
			logger.LogAttrs(ctx, slog.LevelError, "request failed", slog.String("error", err.Error()))
		}
	}
}

// errorLevel maps a typed error to a log level: client-caused (4xx) types are
// logged at Warn, internal or unknown types at Error.
func errorLevel(v *errorx.Error) slog.Level {
	switch v.Type {
	case errorx.NotFoundErrorType, errorx.BadRequestErrorType,
		errorx.BusinessErrorType, errorx.UnauthorizedErrorType,
		errorx.ForbiddenErrorType:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}
