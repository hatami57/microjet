// Package types defines shared data types used across MicroJet, including the
// cursor-paginated result containers.
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

// PagedResultRequest carries pagination parameters for a list query.
//
// It supports two mutually exclusive modes:
//   - Cursor (default): leave Page nil and pass NextPageToken. Forward-only,
//     TotalCount is not computed. Works with any backend.
//   - Offset: set Page (1-based) for arbitrary page jumps. NextPageToken is
//     ignored and TotalCount is populated. Only offset-capable backends (SQL via
//     gormx) honor it; others reject an offset request.
type PagedResultRequest struct {
	PageSize      int32   `json:"pageSize"`
	NextPageToken *string `json:"nextPageToken"`
	Page          *int32  `json:"page"`
}

// ForceOffset selects offset pagination when the caller didn't pick a mode,
// defaulting to the first page. Use it on SQL-backed listing endpoints that
// always want page-number pagination with a computed TotalCount, so a missing
// "page" query param falls back to page 1 instead of cursor mode.
func (r *PagedResultRequest) ForceOffset() *PagedResultRequest {
	if r.Page == nil {
		r.Page = new(int32(1))
	}
	return r
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
