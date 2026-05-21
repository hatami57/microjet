package core

import (
	"errors"
	"fmt"
)

type Error struct {
	Type    ErrorType `json:"type"`
	Subject string    `json:"subject"`
	Message string    `json:"message"`
	Code    int       `json:"code"`
	Inner   error     `json:"inner"`
}

type ErrorType string

const (
	BadRequestErrorType   ErrorType = "BAD_REQUEST"
	NotFoundErrorType     ErrorType = "NOT_FOUND"
	BusinessErrorType     ErrorType = "BUSINESS"
	UnauthorizedErrorType ErrorType = "UNAUTHORIZED"
	ForbiddenErrorType    ErrorType = "FORBIDDEN"
	InternalErrorType     ErrorType = "INTERNAL"
)

func NewError(errorType ErrorType, subject, message string) *Error {
	return &Error{Type: errorType, Subject: subject, Message: message}
}

func NewBadRequestError(subject, message string) *Error {
	return NewError(BadRequestErrorType, subject, message)
}

func NewNotFoundError(subject, message string) *Error {
	return NewError(NotFoundErrorType, subject, message)
}

func NewBusinessError(subject, message string) *Error {
	return NewError(BusinessErrorType, subject, message)
}

func NewUnauthorizedError(subject, message string) *Error {
	return NewError(UnauthorizedErrorType, subject, message)
}

func NewForbiddenError(subject, message string) *Error {
	return NewError(ForbiddenErrorType, subject, message)
}

func NewInternalError(subject, message string) *Error {
	return NewError(InternalErrorType, subject, message)
}

func GetError(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return nil
}

func GetErrorType(err error) (ErrorType, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Type, true
	}
	return "", false
}

func IsBadRequestError(err error) bool   { return isErrorTypeEqual(err, BadRequestErrorType) }
func IsNotFoundError(err error) bool     { return isErrorTypeEqual(err, NotFoundErrorType) }
func IsBusinessError(err error) bool     { return isErrorTypeEqual(err, BusinessErrorType) }
func IsUnauthorizedError(err error) bool { return isErrorTypeEqual(err, UnauthorizedErrorType) }
func IsForbiddenError(err error) bool    { return isErrorTypeEqual(err, ForbiddenErrorType) }
func IsInternalError(err error) bool     { return isErrorTypeEqual(err, InternalErrorType) }

func (e *Error) WithSubject(subject string) *Error { n := *e; n.Subject = subject; return &n }
func (e *Error) WithMessage(message string) *Error { n := *e; n.Message = message; return &n }
func (e *Error) WithCode(code int) *Error          { n := *e; n.Code = code; return &n }
func (e *Error) WithInner(inner error) *Error      { n := *e; n.Inner = inner; return &n }

func (e *Error) Error() string {
	return fmt.Sprintf("[%s] Subject: %s, Message: %s", e.Type, e.Subject, e.Message)
}

func (e *Error) Unwrap() error { return e.Inner }

var (
	ErrBadRequest   = NewBadRequestError("General", "Bad Request")
	ErrNotFound     = NewNotFoundError("General", "Not Found")
	ErrBusiness     = NewBusinessError("General", "Business")
	ErrUnauthorized = NewUnauthorizedError("General", "Unauthorized")
	ErrForbidden    = NewForbiddenError("General", "Forbidden")
	ErrInternal     = NewInternalError("General", "Internal")
)

func isErrorTypeEqual(err error, errType ErrorType) bool {
	t, ok := GetErrorType(err)
	return ok && t == errType
}
