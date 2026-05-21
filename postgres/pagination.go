package postgres

import (
	"fmt"
	"reflect"
	"time"

	"github.com/hatami57/microjet/types"
)

// --- ID-based pagination ---

type ListRequestByID[TEntity any] struct {
	*types.PagedResultRequest
	where    any
	args     []any
	whereMap map[string]any
}

type PageDataByID struct {
	LastID any
}

func NewListRequestByID[TEntity any](req *types.PagedResultRequest) *ListRequestByID[TEntity] {
	return &ListRequestByID[TEntity]{PagedResultRequest: req}
}

func (r *ListRequestByID[TEntity]) SetWhere(where any, args ...any) *ListRequestByID[TEntity] {
	r.where = where
	r.args = args
	return r
}

func (r *ListRequestByID[TEntity]) SetWhereMap(where map[string]any) *ListRequestByID[TEntity] {
	r.whereMap = where
	return r
}

func (r *ListRequestByID[TEntity]) CurrentPageData() (where *string, args []any, err error) {
	pageData, err := types.DecodePageToken[PageDataByID](r.NextPageToken)
	if err != nil {
		return nil, nil, err
	}
	if pageData == nil {
		return nil, nil, nil
	}
	w := "id > ?"
	return &w, []any{pageData.LastID}, nil
}

func (r *ListRequestByID[TEntity]) CreateNextPageToken(items []TEntity) (*string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	lastID, err := getFieldValue[any]("ID", items[len(items)-1])
	if err != nil {
		return nil, err
	}
	return types.EncodePageToken(&PageDataByID{LastID: lastID})
}

func (r *ListRequestByID[TEntity]) Where() (any, []any)    { return r.where, r.args }
func (r *ListRequestByID[TEntity]) WhereMap() map[string]any { return r.whereMap }
func (r *ListRequestByID[TEntity]) PageSize() int           { return int(r.PagedResultRequest.PageSize) }
func (r *ListRequestByID[TEntity]) OrderBy() string         { return "id ASC" }

// --- CreatedAt-based pagination ---

type ListRequestByCreatedAt[TEntity any] struct {
	*types.PagedResultRequest
	where    any
	args     []any
	whereMap map[string]any
}

type PageDataByCreatedAt struct {
	LastCreatedAt time.Time
}

func NewListRequestByCreatedAt[TEntity any](req *types.PagedResultRequest) *ListRequestByCreatedAt[TEntity] {
	return &ListRequestByCreatedAt[TEntity]{PagedResultRequest: req}
}

func (r *ListRequestByCreatedAt[TEntity]) SetWhere(where any, args ...any) *ListRequestByCreatedAt[TEntity] {
	r.where = where
	r.args = args
	return r
}

func (r *ListRequestByCreatedAt[TEntity]) SetWhereMap(where map[string]any) *ListRequestByCreatedAt[TEntity] {
	r.whereMap = where
	return r
}

func (r *ListRequestByCreatedAt[TEntity]) CurrentPageData() (where *string, args []any, err error) {
	pageData, err := types.DecodePageToken[PageDataByCreatedAt](r.NextPageToken)
	if err != nil {
		return nil, nil, err
	}
	if pageData == nil {
		return nil, nil, nil
	}
	w := "created_at > ?"
	return &w, []any{pageData.LastCreatedAt}, nil
}

func (r *ListRequestByCreatedAt[TEntity]) CreateNextPageToken(items []TEntity) (*string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	lastCreatedAt, err := getFieldValue[time.Time]("CreatedAt", items[len(items)-1])
	if err != nil {
		return nil, err
	}
	return types.EncodePageToken(&PageDataByCreatedAt{LastCreatedAt: lastCreatedAt})
}

func (r *ListRequestByCreatedAt[TEntity]) Where() (any, []any)    { return r.where, r.args }
func (r *ListRequestByCreatedAt[TEntity]) WhereMap() map[string]any { return r.whereMap }
func (r *ListRequestByCreatedAt[TEntity]) PageSize() int           { return int(r.PagedResultRequest.PageSize) }
func (r *ListRequestByCreatedAt[TEntity]) OrderBy() string         { return "created_at ASC" }

// --- helpers ---

func getFieldValue[TValue any](fieldName string, obj interface{}) (TValue, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		var zero TValue
		return zero, fmt.Errorf("'%s' field not found", fieldName)
	}
	return field.Interface().(TValue), nil
}
