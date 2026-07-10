package httpx

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core/errorx"
)

type QueryParamBase struct {
	data map[string]any
}

func (q *QueryParamBase) SetField(name string, value any) {
	if q.data == nil {
		q.data = make(map[string]any)
	}
	q.data[name] = value
}

func (q *QueryParamBase) GetMap() map[string]any { return q.data }

// BindQueryParams maps query parameters onto the tagged fields of target (a
// pointer to a struct) and records them in the param map. A malformed value
// (e.g. ?id=notauuid) returns a core.BadRequest error naming the offending
// parameter rather than silently substituting a zero value.
func (q *QueryParamBase) BindQueryParams(c *gin.Context, target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("BindQueryParams: target must be a non-nil pointer to a struct, got %T", target)
	}
	v := rv.Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		queryTag := fieldType.Tag.Get("query")
		if queryTag == "" {
			continue
		}
		mapKey := fieldType.Tag.Get("mapKey")
		if mapKey == "" {
			mapKey = queryTag
		}
		defaultTag := fieldType.Tag.Get("default")

		value, exists := c.GetQuery(queryTag)
		if !exists {
			if defaultTag == "" {
				continue
			}
			value = defaultTag
		}

		fv, err := parseQueryField(field, value)
		if err != nil {
			return errorx.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid value for query param '%s'", queryTag))
		}
		q.SetField(mapKey, fv)
	}
	return nil
}

func parseQueryField(field reflect.Value, queryValue string) (any, error) {
	if !field.CanSet() {
		return nil, fmt.Errorf("cannot set field")
	}
	if queryValue == "" {
		field.SetZero()
		return field.Interface(), nil
	}
	if field.Type() == reflect.TypeOf(uuid.UUID{}) {
		uid, err := uuid.Parse(queryValue)
		if err != nil {
			return uuid.Nil, err
		}
		return uid, nil
	}
	switch field.Kind() {
	case reflect.String:
		return queryValue, nil
	case reflect.Int, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(queryValue, 10, 64)
		if err == nil {
			field.SetInt(i)
		}
		return field.Interface(), err
	case reflect.Pointer:
		if field.Type().Elem().Kind() == reflect.Int32 {
			i, err := strconv.ParseInt(queryValue, 10, 32)
			if err == nil {
				val := int32(i)
				field.Set(reflect.ValueOf(&val))
			}
			return field.Interface(), err
		}
	}
	return nil, fmt.Errorf("unhandled type: %s", field.Kind())
}
