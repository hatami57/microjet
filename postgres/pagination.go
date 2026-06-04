package postgres

import (
	"fmt"
	"reflect"
	"time"

	"github.com/hatami57/microjet/types"
)

// --- Generic typed-cursor pagination (preferred) ---

// PageRequest implements cursor-based pagination for Table.List.
// TEntity is the row type; TValue is the type of the cursor column (e.g. time.Time, int64).
// Using a typed cursor avoids the JSON float64 coercion that occurs when TValue is any.
//
// Example:
//
//	req := postgres.NewPageRequest(
//	    pagedReq,                          // *types.PagedResultRequest from the HTTP handler
//	    "created_at",                      // cursor column
//	    func(e Order) time.Time { return e.CreatedAt },
//	).SetWhere("tenant_id = ?", tenantID).OrderDesc()
//
//	result, err := orderTable.List(ctx, req)
type PageRequest[TEntity any, TValue any] struct {
	*types.PagedResultRequest
	where     []any
	cursorCol string
	cursorFn  func(TEntity) TValue
	desc      bool
}

// NewPageRequest creates a PageRequest.
// cursorCol is the SQL column name used for ordering and cursor position (e.g. "id", "created_at").
// cursorFn extracts the cursor value from a row; its return type must match TValue.
func NewPageRequest[TEntity any, TValue any](
	req *types.PagedResultRequest,
	cursorCol string,
	cursorFn func(TEntity) TValue,
) *PageRequest[TEntity, TValue] {
	return &PageRequest[TEntity, TValue]{
		PagedResultRequest: req,
		cursorCol:          cursorCol,
		cursorFn:           cursorFn,
	}
}

// SetWhere sets the WHERE filter condition. Replaces any previously set condition.
// Accepts a string condition + args or a map[string]any.
func (r *PageRequest[TEntity, TValue]) SetWhere(condition any, args ...any) *PageRequest[TEntity, TValue] {
	r.where = append([]any{condition}, args...)
	return r
}

// OrderDesc switches the sort order to descending and flips the cursor operator.
// By default results are ordered ascending.
func (r *PageRequest[TEntity, TValue]) OrderDesc() *PageRequest[TEntity, TValue] {
	r.desc = true
	return r
}

func (r *PageRequest[TEntity, TValue]) Where() []any { return r.where }

func (r *PageRequest[TEntity, TValue]) OrderBy() string {
	if r.desc {
		return r.cursorCol + " DESC"
	}
	return r.cursorCol + " ASC"
}

func (r *PageRequest[TEntity, TValue]) PageSize() int { return int(r.PagedResultRequest.PageSize) }

type pageData[TValue any] struct {
	LastValue TValue `json:"v"`
}

func (r *PageRequest[TEntity, TValue]) CurrentPageData() ([]any, error) {
	data, err := types.DecodePageToken[pageData[TValue]](r.NextPageToken)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	op := ">"
	if r.desc {
		op = "<"
	}
	return []any{fmt.Sprintf("%s %s ?", r.cursorCol, op), data.LastValue}, nil
}

func (r *PageRequest[TEntity, TValue]) CreateNextPageToken(items []TEntity) (*string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	return types.EncodePageToken(pageData[TValue]{LastValue: r.cursorFn(items[len(items)-1])})
}

// --- ID-based pagination (untyped cursor — float64 coercion applies after JSON round-trip) ---

type ListRequestByID[TEntity any] struct {
	*types.PagedResultRequest
	where []any
}

type pageDataByID struct {
	LastID any `json:"id"`
}

func NewListRequestByID[TEntity any](req *types.PagedResultRequest) *ListRequestByID[TEntity] {
	return &ListRequestByID[TEntity]{PagedResultRequest: req}
}

// SetWhere sets the WHERE filter. Accepts a string condition + args or a map[string]any.
func (r *ListRequestByID[TEntity]) SetWhere(condition any, args ...any) *ListRequestByID[TEntity] {
	r.where = append([]any{condition}, args...)
	return r
}

func (r *ListRequestByID[TEntity]) CurrentPageData() ([]any, error) {
	data, err := types.DecodePageToken[pageDataByID](r.NextPageToken)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return []any{"id > ?", data.LastID}, nil
}

func (r *ListRequestByID[TEntity]) CreateNextPageToken(items []TEntity) (*string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	lastID, err := getFieldValue[any]("ID", items[len(items)-1])
	if err != nil {
		return nil, err
	}
	return types.EncodePageToken(&pageDataByID{LastID: lastID})
}

func (r *ListRequestByID[TEntity]) Where() []any      { return r.where }
func (r *ListRequestByID[TEntity]) PageSize() int      { return int(r.PagedResultRequest.PageSize) }
func (r *ListRequestByID[TEntity]) OrderBy() string    { return "id ASC" }

// --- CreatedAt-based pagination ---

type ListRequestByCreatedAt[TEntity any] struct {
	*types.PagedResultRequest
	where []any
}

type pageDataByCreatedAt struct {
	LastCreatedAt time.Time `json:"createdAt"`
}

func NewListRequestByCreatedAt[TEntity any](req *types.PagedResultRequest) *ListRequestByCreatedAt[TEntity] {
	return &ListRequestByCreatedAt[TEntity]{PagedResultRequest: req}
}

func (r *ListRequestByCreatedAt[TEntity]) SetWhere(condition any, args ...any) *ListRequestByCreatedAt[TEntity] {
	r.where = append([]any{condition}, args...)
	return r
}

func (r *ListRequestByCreatedAt[TEntity]) CurrentPageData() ([]any, error) {
	data, err := types.DecodePageToken[pageDataByCreatedAt](r.NextPageToken)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return []any{"created_at > ?", data.LastCreatedAt}, nil
}

func (r *ListRequestByCreatedAt[TEntity]) CreateNextPageToken(items []TEntity) (*string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	lastCreatedAt, err := getFieldValue[time.Time]("CreatedAt", items[len(items)-1])
	if err != nil {
		return nil, err
	}
	return types.EncodePageToken(&pageDataByCreatedAt{LastCreatedAt: lastCreatedAt})
}

func (r *ListRequestByCreatedAt[TEntity]) Where() []any      { return r.where }
func (r *ListRequestByCreatedAt[TEntity]) PageSize() int      { return int(r.PagedResultRequest.PageSize) }
func (r *ListRequestByCreatedAt[TEntity]) OrderBy() string    { return "created_at ASC" }

// --- helpers ---

func getFieldValue[TValue any](fieldName string, obj any) (TValue, error) {
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
