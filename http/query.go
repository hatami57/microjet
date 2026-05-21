package httpx

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

func (q *QueryParamBase) BindQueryParams(c *gin.Context, target any) {
	v := reflect.ValueOf(target).Elem()
	t := reflect.TypeOf(target).Elem()

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

		queryValue, exists := c.GetQuery(queryTag)
		if exists {
			if fv, err := parseQueryField(field, queryValue); err == nil {
				q.SetField(mapKey, fv)
			}
		} else if defaultTag != "" {
			if fv, err := parseQueryField(field, defaultTag); err == nil {
				q.SetField(mapKey, fv)
			}
		}
	}
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
	case reflect.Ptr:
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
