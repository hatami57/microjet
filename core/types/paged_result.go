package types

import (
	"encoding/base64"

	jsoniter "github.com/json-iterator/go"
)

type PagedResult[T any] struct {
	Items         []T     `json:"items"`
	PageSize      int32   `json:"pageSize"`
	TotalCount    int64   `json:"totalCount"`
	NextPageToken *string `json:"nextPageToken"`
}

type PagedResultRequest struct {
	PageSize      int32   `json:"pageSize"`
	NextPageToken *string `json:"nextPageToken"`
}

func EncodePageToken[T any](v T) (*string, error) {
	jsonBytes, err := jsoniter.Marshal(v)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(jsonBytes)
	return &encoded, nil
}

func DecodePageToken[T any](token *string) (*T, error) {
	if token == nil || len(*token) == 0 {
		return nil, nil
	}
	jsonBytes, err := base64.StdEncoding.DecodeString(*token)
	if err != nil {
		return nil, err
	}
	var v T
	if err = jsoniter.Unmarshal(jsonBytes, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
