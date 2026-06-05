package utils

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

func Coalesce[T any](ptrs ...*T) *T {
	for _, p := range ptrs {
		if p != nil {
			return p
		}
	}
	return nil
}

func CoalesceVal[T any](def T, ptrs ...*T) T {
	for _, p := range ptrs {
		if p != nil {
			return *p
		}
	}
	return def
}
