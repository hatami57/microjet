package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/hatami57/microjet/core/errorx"
)

var jsonFieldNamesOnce sync.Once

// UseJSONFieldNames makes gin's binding validator report the json tag name
// (e.g. "emailAddress") rather than the Go struct field name in validation
// errors, so the per-field details returned by ValidationError match the keys
// in the request body. It is applied automatically by Body; call it directly
// only if you bind through gin yourself. Safe to call repeatedly and concurrently.
func UseJSONFieldNames() {
	jsonFieldNamesOnce.Do(func() {
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(func(field reflect.StructField) string {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				return field.Name
			}
			return name
		})
	})
}

// ValidationError converts a gin binding/validation error into a structured
// BadRequest *errorx.Error. A validator failure carries a per-field breakdown
// (field name → reason) under the "fields" param; a JSON type mismatch names
// the offending field; anything else falls back to the raw decode message. The
// returned error matches errors.Is(err, errorx.ErrBadRequest).
func ValidationError(err error) *errorx.Error {
	if verrs, ok := errors.AsType[validator.ValidationErrors](err); ok {
		fields := make(map[string]any, len(verrs))
		for _, fe := range verrs {
			fields[fe.Field()] = validationMessage(fe)
		}
		return errorx.ErrBadRequest.WithMessage("Validation failed").WithParams("fields", fields)
	}

	if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		field := typeErr.Field
		if field == "" {
			field = "body"
		}
		return errorx.ErrBadRequest.WithMessage(
			fmt.Sprintf("Invalid type for field %q: expected %s", field, typeErr.Type.String()))
	}

	return errorx.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid body: %s", err.Error()))
}

// validationMessage renders a human-readable reason for a single field error,
// covering the common validator tags and falling back to a generic message.
func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fe.Param()
	case "max":
		return "must be at most " + fe.Param()
	case "len":
		return "must have length " + fe.Param()
	case "oneof":
		return "must be one of: " + fe.Param()
	case "uuid", "uuid4":
		return "must be a valid UUID"
	case "gt":
		return "must be greater than " + fe.Param()
	case "gte":
		return "must be greater than or equal to " + fe.Param()
	case "lt":
		return "must be less than " + fe.Param()
	case "lte":
		return "must be less than or equal to " + fe.Param()
	default:
		if fe.Param() != "" {
			return fmt.Sprintf("failed %q validation (%s)", fe.Tag(), fe.Param())
		}
		return fmt.Sprintf("failed %q validation", fe.Tag())
	}
}
