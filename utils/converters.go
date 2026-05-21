package utils

import jsoniter "github.com/json-iterator/go"

func ToMap[T any](t T) (map[string]interface{}, error) {
	data, err := jsoniter.Marshal(t)
	if err != nil {
		return nil, err
	}
	decoded := make(map[string]interface{})
	err = jsoniter.Unmarshal(data, &decoded)
	return decoded, err
}

func MapTo[T any](m map[string]interface{}) (T, error) {
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

func ToMapArray[T any](t []T) ([]map[string]interface{}, error) {
	data, err := jsoniter.Marshal(t)
	if err != nil {
		return nil, err
	}
	var decoded []map[string]interface{}
	err = jsoniter.Unmarshal(data, &decoded)
	return decoded, err
}

func MapToArray[T any](m []map[string]interface{}) ([]T, error) {
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
