package gormx

import (
	"fmt"

	"github.com/hatami57/microjet/core/types"
)

type pageData[TValue any] struct {
	LastValue TValue `json:"v"`
}

// PageRequest implements cursor-based pagination for Table.List.
// TEntity is the row type; TValue is the type of the cursor column (e.g. time.Time, int64).
// Using a typed cursor avoids the JSON float64 coercion issue that occurs with any.
//
// Example:
//
//	req := db.NewPageRequest(
//	    pagedReq,                          // *types.PagedResultRequest from the HTTP handler
//	    "created_at",                      // cursor column
//	    func(e Order) time.Time { return e.CreatedAt },
//	).OrderDesc()
//
//	result, err := orderTable.Where("tenant_id = ?", tenantID).List(ctx, req)
type PageRequest[TEntity any, TValue any] struct {
	*types.PagedResultRequest
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

// OrderDesc switches the sort order to descending and flips the cursor operator accordingly.
// By default results are ordered ascending.
func (r *PageRequest[TEntity, TValue]) OrderDesc() *PageRequest[TEntity, TValue] {
	r.desc = true
	return r
}

func (r *PageRequest[TEntity, TValue]) OrderBy() string {
	if r.desc {
		return r.cursorCol + " DESC"
	}
	return r.cursorCol + " ASC"
}

func (r *PageRequest[TEntity, TValue]) PageSize() int {
	return int(r.PagedResultRequest.PageSize)
}

// Offset reports the row offset for page-number pagination. ok is false when no
// Page is set, in which case Table.List uses cursor pagination instead. Page is
// 1-based; values below 1 are clamped to the first page.
func (r *PageRequest[TEntity, TValue]) Offset() (offset int, ok bool) {
	if r.Page == nil {
		return 0, false
	}
	page := max(int(*r.Page), 1)
	return (page - 1) * r.PageSize(), true
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
