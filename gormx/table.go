// Package gormx provides GORM-based table helpers with pagination support.
package gormx

import (
	"context"
	"errors"

	"github.com/hatami57/microjet/core/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type txKey struct{}

// BaseRepository holds a database connection and provides RunTx for transaction management.
// Embed it in your own repository structs to inherit transaction support without
// implementing it yourself.
//
// Example:
//
//	type OrderRepo struct {
//	    db.BaseRepository
//	    orders   *db.Table[Order]
//	    payments *db.Table[Payment]
//	}
//
//	func NewOrderRepo(h *mjhost.App) (*OrderRepo, error) {
//	    base, err := db.NewBaseRepository(h)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return &OrderRepo{
//	        BaseRepository: *base,
//	        orders:         db.NewTableFor[Order](base),
//	        payments:       db.NewTableFor[Payment](base),
//	    }, nil
//	}
//
//	func (s *OrderService) PlaceOrder(ctx context.Context, ...) error {
//	    return s.repo.RunTx(ctx, func(ctx context.Context) error {
//	        ...
//	    })
//	}
type BaseRepository struct {
	gormDB *gorm.DB
}

func NewBaseRepository(db *gorm.DB) BaseRepository {
	return BaseRepository{gormDB: db}
}

// RunTx executes op inside a database transaction, rolling back on any error.
// If a transaction is already present in ctx, op runs within that transaction
// (propagation required — the outermost RunTx owns commit/rollback).
func (r *BaseRepository) RunTx(ctx context.Context, op func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return op(ctx)
	}
	return r.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return op(context.WithValue(ctx, txKey{}, tx))
	})
}

// NewTableFor creates a Table derived from the same database connection as the BaseRepository.
// Use this during repository construction so all tables share the same connection.
func NewTableFor[TEntity any](r *BaseRepository) *Table[TEntity] {
	return NewTable[TEntity](r.gormDB)
}

type preloadEntry struct {
	query string
	args  []any
}

// Table is a generic GORM wrapper for a single database table.
// Create one per entity type via NewTable.
// Call Preload to eager-load associations before executing a query.
// Call WhereIf to accumulate conditional WHERE clauses.
type Table[TEntity any] struct {
	entity   *TEntity
	gormDB   *gorm.DB
	preloads []preloadEntry
	scopes   []func(*gorm.DB) *gorm.DB
}

// ListRequest is the interface Table.List requires.
// Use PageRequest[TEntity, TValue] for cursor-based pagination, or implement this
// interface directly for custom pagination strategies.
type ListRequest[T any] interface {
	CurrentPageData() (where []any, err error)
	CreateNextPageToken(items []T) (*string, error)
	PageSize() int
	OrderBy() string
	Where() []any
}

// Scoper is an optional interface for ListRequest.
// If implemented, Scope() takes precedence over Where().
type Scoper interface {
	Scope() func(*gorm.DB) *gorm.DB
}

// CursorScoper is an optional interface for ListRequest.
// If implemented, CursorScope() takes precedence over CurrentPageData().
type CursorScoper interface {
	CursorScope() (func(*gorm.DB) *gorm.DB, error)
}

// BaseListRequest provides default no-op implementations of all ListRequest methods.
// Embed it in your own request struct and override only the methods you need.
//
// Example:
//
//	type MyRequest struct {
//	    db.BaseListRequest[MyEntity]
//	    TenantID string
//	}
//	func (r *MyRequest) Where() []any { return []any{"tenant_id = ?", r.TenantID} }
type BaseListRequest[T any] struct {
	pageSize int
	orderBy  string
}

func NewBaseListRequest[T any](pageSize int, orderBy string) BaseListRequest[T] {
	return BaseListRequest[T]{pageSize: pageSize, orderBy: orderBy}
}

func (b *BaseListRequest[T]) PageSize() int                              { return b.pageSize }
func (b *BaseListRequest[T]) OrderBy() string                            { return b.orderBy }
func (b *BaseListRequest[T]) Where() []any                               { return nil }
func (b *BaseListRequest[T]) CurrentPageData() ([]any, error)            { return nil, nil }
func (b *BaseListRequest[T]) CreateNextPageToken(_ []T) (*string, error) { return nil, nil }

// NewTable creates a Table for TEntity backed by a database.
func NewTable[TEntity any](db *gorm.DB) *Table[TEntity] {
	var entity TEntity
	return &Table[TEntity]{entity: &entity, gormDB: db}
}

// Preload returns a copy of the Table that eager-loads association on every query.
// args mirrors GORM's Preload: a SQL condition string + values, or a func(*gorm.DB) *gorm.DB,
// or nothing for an unconditional preload. Use clause.Associations ("*") to preload all.
// Multiple calls accumulate; preloads do not modify the original Table.
func (t *Table[TEntity]) Preload(association string, args ...any) *Table[TEntity] {
	entry := preloadEntry{query: association, args: args}
	return &Table[TEntity]{
		entity:   t.entity,
		gormDB:   t.gormDB,
		preloads: append(append([]preloadEntry{}, t.preloads...), entry),
		scopes:   t.scopes,
	}
}

// Where returns a copy of the Table with an additional WHERE clause. Calls accumulate;
// the original Table is not modified. Accepts the same formats as gorm.DB.Where.
func (t *Table[TEntity]) Where(query any, args ...any) *Table[TEntity] {
	return &Table[TEntity]{
		entity:   t.entity,
		gormDB:   t.gormDB,
		preloads: t.preloads,
		scopes: append(append([]func(*gorm.DB) *gorm.DB{}, t.scopes...), func(db *gorm.DB) *gorm.DB {
			return db.Where(query, args...)
		}),
	}
}

// WhereIf returns a copy of the Table with an additional WHERE clause that is applied
// only when condition is true. Calls accumulate; the original Table is not modified.
//
// Example:
//
//	results, err := table.
//	    WhereIf(req.TenantID != "", "tenant_id = ?", req.TenantID).
//	    WhereIf(req.Active, "active = true").
//	    ListAll(ctx, "created_at DESC")
func (t *Table[TEntity]) WhereIf(condition bool, where ...any) *Table[TEntity] {
	if !condition {
		return t
	}
	return &Table[TEntity]{
		entity:   t.entity,
		gormDB:   t.gormDB,
		preloads: t.preloads,
		scopes: append(append([]func(*gorm.DB) *gorm.DB{}, t.scopes...), func(db *gorm.DB) *gorm.DB {
			return db.Where(where[0], where[1:]...)
		}),
	}
}

func (t *Table[TEntity]) db(ctx context.Context) *gorm.DB {
	var base *gorm.DB
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		base = tx.WithContext(ctx)
	} else {
		base = t.gormDB.WithContext(ctx)
	}
	for _, s := range t.scopes {
		base = s(base)
	}
	return base
}

func (t *Table[TEntity]) applyPreloads(db *gorm.DB) *gorm.DB {
	for _, p := range t.preloads {
		db = db.Preload(p.query, p.args...)
	}
	return db
}

// Create inserts a new row. GORM populates auto-generated fields (ID, CreatedAt, etc.) on item.
func (t *Table[TEntity]) Create(ctx context.Context, item *TEntity) error {
	return t.db(ctx).Create(item).Error
}

// CreateMany inserts items in batches of batchSize. Use for bulk inserts.
func (t *Table[TEntity]) CreateMany(ctx context.Context, items []*TEntity, batchSize int) error {
	return t.db(ctx).CreateInBatches(items, batchSize).Error
}

// Save performs a full upsert — all fields are written. Use Update or UpdateMap for partial updates.
func (t *Table[TEntity]) Save(ctx context.Context, item *TEntity) error {
	return t.db(ctx).Save(item).Error
}

// Upsert inserts item or updates all columns if the primary key already exists.
// Uses a database-level ON CONFLICT clause, making it safe under concurrent writes.
func (t *Table[TEntity]) Upsert(ctx context.Context, item *TEntity) error {
	return t.db(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(item).Error
}

// Update performs a partial update — only non-zero fields in item are written.
func (t *Table[TEntity]) Update(ctx context.Context, item *TEntity) error {
	return t.db(ctx).Updates(item).Error
}

// UpdateMap applies a column→value map as a partial update. Accepts optional GORM-style
// where conditions (same format as Find/Count) to scope which rows are updated.
func (t *Table[TEntity]) UpdateMap(ctx context.Context, values map[string]any, where ...any) error {
	q := t.db(ctx).Model(t.entity)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	return q.Updates(values).Error
}

// Delete removes rows matching conditions. Accepts the same GORM condition formats as Find.
func (t *Table[TEntity]) Delete(ctx context.Context, conditions ...any) error {
	return t.db(ctx).Delete(t.entity, conditions...).Error
}

// Count returns the number of rows matching the optional where conditions.
func (t *Table[TEntity]) Count(ctx context.Context, where ...any) (int64, error) {
	var count int64
	q := t.db(ctx).Model(t.entity)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// PluckDistinct collects unique values from column into dest (a pointer to a slice).
// Accepts optional GORM-style where conditions to scope the query.
func (t *Table[TEntity]) PluckDistinct(ctx context.Context, column string, dest any, where ...any) error {
	q := t.db(ctx).Model(t.entity).Distinct(column)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	return q.Pluck(column, dest).Error
}

// Find returns the first row matching conditions, or nil if not found (no error).
// Accepts GORM condition formats: primary key value, "col = ?", or map[string]any.
func (t *Table[TEntity]) Find(ctx context.Context, conditions ...any) (*TEntity, error) {
	var item TEntity
	err := t.applyPreloads(t.db(ctx)).First(&item, conditions...).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

// PageOptions configures a ListPage call. Build it fluently with NewPageOptions:
//
//	NewPageOptions[Order]().PageSize(20).OrderBy("created_at DESC")
//
// For cursor-based pagination, also set Cursor (the current-page filter) and
// NextToken (builds the token for the following page):
//
//	NewPageOptions[Order]().
//	    PageSize(20).
//	    OrderBy("created_at DESC").
//	    Cursor(func() ([]any, error) {
//	        if token == "" {
//	            return nil, nil
//	        }
//	        return []any{"created_at < ?", token}, nil
//	    }).
//	    NextToken(func(items []Order) (*string, error) {
//	        s := items[len(items)-1].CreatedAt.Format(time.RFC3339)
//	        return &s, nil
//	    })
type PageOptions[T any] struct {
	pageSize  int
	orderBy   string
	cursor    func() ([]any, error)
	nextToken func(items []T) (*string, error)
}

// NewPageOptions returns an empty PageOptions ready to configure.
func NewPageOptions[T any]() *PageOptions[T] { return &PageOptions[T]{} }

// PageSize sets the maximum number of rows returned and returns o for chaining.
func (o *PageOptions[T]) PageSize(n int) *PageOptions[T] { o.pageSize = n; return o }

// OrderBy sets the ORDER BY clause and returns o for chaining.
func (o *PageOptions[T]) OrderBy(s string) *PageOptions[T] { o.orderBy = s; return o }

// Cursor sets the WHERE condition that filters to the current page; decode the page
// token here and return it as {query, args...}, or nil for the first page. It runs
// after the table's accumulated Where/WhereIf scopes.
func (o *PageOptions[T]) Cursor(fn func() ([]any, error)) *PageOptions[T] {
	o.cursor = fn
	return o
}

// NextToken sets the function that builds the next-page token from the page's items.
// When set, ListPage fetches one extra row to decide whether a next page exists.
func (o *PageOptions[T]) NextToken(fn func(items []T) (*string, error)) *PageOptions[T] {
	o.nextToken = fn
	return o
}

// ListPage returns one page of results using the table's accumulated scopes.
// Chain Where/WhereIf/Preload on the Table to filter and eager-load before calling.
// Set Cursor/NextToken on opts for cursor-based pagination; otherwise NextPageToken is nil.
func (t *Table[TEntity]) ListPage(ctx context.Context, opts *PageOptions[TEntity]) (*types.PagedResult[TEntity], error) {
	if opts == nil {
		opts = NewPageOptions[TEntity]()
	}

	q := t.applyPreloads(t.db(ctx)).Order(opts.orderBy)
	if opts.cursor != nil {
		where, err := opts.cursor()
		if err != nil {
			return nil, err
		}
		if len(where) > 0 {
			q = q.Where(where[0], where[1:]...)
		}
	}

	// Fetch one extra row to detect a following page when generating a token.
	limit := opts.pageSize
	if opts.nextToken != nil && limit > 0 {
		limit++
	}
	if limit > 0 {
		q = q.Limit(limit)
	}

	var items []TEntity
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}

	var nextPageToken *string
	if opts.nextToken != nil && opts.pageSize > 0 && len(items) > opts.pageSize {
		items = items[:opts.pageSize]
		var err error
		if nextPageToken, err = opts.nextToken(items); err != nil {
			return nil, err
		}
	}

	return &types.PagedResult[TEntity]{
		Items:         items,
		PageSize:      int32(opts.pageSize),
		TotalCount:    -1,
		NextPageToken: nextPageToken,
	}, nil
}

// List returns one page of results. TotalCount is always -1 (not computed).
// If the request implements Scoper, its Scope() replaces Where().
// If it implements CursorScoper, its CursorScope() replaces CurrentPageData().
func (t *Table[TEntity]) List(ctx context.Context, req ListRequest[TEntity]) (*types.PagedResult[TEntity], error) {
	pageSize := req.PageSize()

	q := t.applyPreloads(t.db(ctx)).Order(req.OrderBy()).Limit(pageSize + 1)
	if sr, ok := any(req).(Scoper); ok {
		q = q.Scopes(sr.Scope())
	} else if where := req.Where(); len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	if cs, ok := any(req).(CursorScoper); ok {
		scope, err := cs.CursorScope()
		if err != nil {
			return nil, err
		}
		if scope != nil {
			q = q.Scopes(scope)
		}
	} else {
		cursorWhere, err := req.CurrentPageData()
		if err != nil {
			return nil, err
		}
		if len(cursorWhere) > 0 {
			q = q.Where(cursorWhere[0], cursorWhere[1:]...)
		}
	}

	var items []TEntity
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}

	var nextPageToken *string
	if len(items) > pageSize {
		items = items[:pageSize]
		var err error
		nextPageToken, err = req.CreateNextPageToken(items)
		if err != nil {
			return nil, err
		}
	}

	return &types.PagedResult[TEntity]{
		Items:         items,
		PageSize:      int32(pageSize),
		TotalCount:    -1,
		NextPageToken: nextPageToken,
	}, nil
}

// ListAll returns all matching rows. Use only when the result set is known to be small.
// Chain Where/WhereIf/Preload on the Table to filter and eager-load before calling.
func (t *Table[TEntity]) ListAll(ctx context.Context, orderBy string) ([]*TEntity, error) {
	q := t.applyPreloads(t.db(ctx)).Order(orderBy)
	var items []*TEntity
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
