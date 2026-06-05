package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type Error struct {
	Type    ErrorType      `json:"type"`
	Subject string         `json:"subject"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
	Code    int            `json:"code"`
	Inner   error          `json:"-"`
}

// ErrorType identifies the category of an error and controls HTTP status mapping:
// BadRequest→400, Unauthorized→401, Forbidden→403, NotFound→404, Business→409, Internal→500.
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

// WithMessage returns a copy of the error with the message replaced.
// Optional key-value pairs are merged into Params (same semantics as WithParams).
func (e *Error) WithMessage(message string, params ...any) *Error {
	n := e.WithParams(params...)
	n.Message = message
	return n
}

// WithParams returns a copy of the error with additional key-value pairs merged into Params.
// Keys must be strings; non-string keys are silently skipped.
func (e *Error) WithParams(keyvals ...any) *Error {
	if len(keyvals) == 0 && len(e.Params) == 0 {
		n := *e
		return &n
	}
	newParams := make(map[string]any, len(e.Params)+len(keyvals)/2)
	maps.Copy(newParams, e.Params)
	copySliceToMap(keyvals, newParams)
	n := *e
	if len(newParams) == 0 {
		n.Params = nil
	} else {
		n.Params = newParams
	}
	return &n
}

func (e *Error) WithCode(code int) *Error     { n := *e; n.Code = code; return &n }
func (e *Error) WithInner(inner error) *Error { n := *e; n.Inner = inner; return &n }

func (e *Error) Error() string {
	var s strings.Builder
	fmt.Fprintf(&s, "[%s] Subject: %s, Message: %s", e.Type, e.Subject, e.Message)
	keys := make([]string, 0, len(e.Params))
	for k := range e.Params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(&s, ", %s=%v", k, e.Params[k])
	}
	if e.Inner != nil {
		fmt.Fprintf(&s, ", inner=%s", e.Inner.Error())
	}
	return s.String()
}

func (e *Error) MarshalJSON() ([]byte, error) {
	type alias Error
	v := struct {
		*alias
		Inner string `json:"inner,omitempty"`
	}{alias: (*alias)(e)}
	if e.Inner != nil {
		v.Inner = e.Inner.Error()
	}
	return json.Marshal(v)
}

func (e *Error) Unwrap() error { return e.Inner }

// Is reports whether e matches target for errors.Is, letting a typed *Error be
// used as a sentinel by category. target matches when it is an *Error of the
// same Type; if target also sets a non-zero Code it must match, and if target
// sets a Subject other than the default "General" it must match too. This makes
// the package sentinels match any error of their category —
// errors.Is(err, ErrNotFound) is true for any NotFound error — while a custom
// sentinel carrying a Subject and/or Code matches more narrowly. Wrapped
// non-Error sentinels still match through Unwrap as usual.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	if t.Type != e.Type {
		return false
	}
	if t.Code != 0 && t.Code != e.Code {
		return false
	}
	if t.Subject != "" && t.Subject != "General" && t.Subject != e.Subject {
		return false
	}
	return true
}

// ErrorResponse is the JSON body returned by the HTTP error middleware.
type ErrorResponse struct {
	Error      string         `json:"error"`
	Subject    string         `json:"subject"`
	Message    string         `json:"message"`
	Params     map[string]any `json:"params,omitempty"`
	Code       int            `json:"code"`
	InnerError *string        `json:"innerError,omitempty"`
}

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

func copySliceToMap(src []any, dst map[string]any) {
	for i := 0; i+1 < len(src); i += 2 {
		// Mirror slog's behavior: a non-string key is a programming error, so
		// surface it under "!BADKEY" rather than silently dropping the value.
		if key, ok := src[i].(string); ok {
			dst[key] = src[i+1]
		} else {
			dst["!BADKEY"] = src[i+1]
		}
	}
}
