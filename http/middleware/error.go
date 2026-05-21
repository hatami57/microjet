package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core"
)

type ErrorResponse struct {
	Error        string  `json:"error"`
	Subject      string  `json:"subject"`
	Message      string  `json:"message"`
	Code         int     `json:"code"`
	InnerMessage *string `json:"innerMessage"`
}

func Error() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		err := c.Errors.Last().Err
		status := http.StatusInternalServerError

		var response ErrorResponse
		var v *core.Error
		switch {
		case errors.As(err, &v):
			var innerMessage *string
			if v.Inner != nil {
				inner := v.Inner.Error()
				innerMessage = &inner
			}
			switch v.Type {
			case core.NotFoundErrorType:
				status = http.StatusNotFound
				response = ErrorResponse{Error: "not_found", Subject: v.Subject, Message: v.Message, Code: v.Code, InnerMessage: innerMessage}
			case core.BadRequestErrorType:
				status = http.StatusBadRequest
				response = ErrorResponse{Error: "invalid_input", Subject: v.Subject, Message: v.Message, Code: v.Code, InnerMessage: innerMessage}
			case core.BusinessErrorType:
				status = http.StatusConflict
				response = ErrorResponse{Error: "conflict", Subject: v.Subject, Message: v.Message, Code: v.Code, InnerMessage: innerMessage}
			case core.UnauthorizedErrorType:
				status = http.StatusUnauthorized
				response = ErrorResponse{Error: "unauthorized", Subject: v.Subject, Message: v.Message, Code: v.Code, InnerMessage: innerMessage}
			case core.ForbiddenErrorType:
				status = http.StatusForbidden
				response = ErrorResponse{Error: "forbidden", Subject: v.Subject, Message: v.Message, Code: v.Code, InnerMessage: innerMessage}
			default:
				response = ErrorResponse{Error: "internal_server_error", Subject: v.Subject, Message: v.Message, Code: v.Code, InnerMessage: innerMessage}
			}
		default:
			errorString := err.Error()
			response = ErrorResponse{Error: "internal_server_error", Subject: "Unknown", Message: "An internal server error occurred", Code: 0, InnerMessage: &errorString}
		}

		c.JSON(status, response)
		c.Abort()
	}
}
