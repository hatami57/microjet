// Package jsonx provides generic helpers for JSON encoding/decoding and
// conversion between structs and maps.
package jsonx

import jsoniter "github.com/json-iterator/go"

func ToJSON[T any](data T) (string, error) {
	bytes, err := jsoniter.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func FromJSON[T any](jsonStr string, target T) error {
	return jsoniter.Unmarshal([]byte(jsonStr), &target)
}

func ToMap[T any](t T) (map[string]any, error) {
	data, err := jsoniter.Marshal(t)
	if err != nil {
		return nil, err
	}
	decoded := make(map[string]any)
	err = jsoniter.Unmarshal(data, &decoded)
	return decoded, err
}

func MapTo[T any](m map[string]any) (T, error) {
	var result T
	data, err := jsoniter.Marshal(m)
	if err != nil {
		return result, err
	}
	if err := jsoniter.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, nil
}

func ToMapArray[T any](t []T) ([]map[string]any, error) {
	data, err := jsoniter.Marshal(t)
	if err != nil {
		return nil, err
	}
	var decoded []map[string]any
	err = jsoniter.Unmarshal(data, &decoded)
	return decoded, err
}

func MapToArray[T any](m []map[string]any) ([]T, error) {
	var result []T
	data, err := jsoniter.Marshal(m)
	if err != nil {
		return nil, err
	}
	if err := jsoniter.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func AnyToStruct(data any, target any) error {
	bytes, err := jsoniter.Marshal(data)
	if err != nil {
		return err
	}
	return jsoniter.Unmarshal(bytes, target)
}
