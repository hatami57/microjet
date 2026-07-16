// Package gormx provides GORM-based table helpers with pagination support.
package gormx

import (
	"context"
	"errors"
	"reflect"

	"github.com/hatami57/microjet/core/errorx"
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
}

// Scoper is an optional interface for ListRequest. If implemented, its Scope() is
// applied on top of the Table's accumulated Where/WhereIf scopes — use it for
// request-driven filtering that can't be expressed by chaining Where on the Table.
type Scoper interface {
	Scope() func(*gorm.DB) *gorm.DB
}

// CursorScoper is an optional interface for ListRequest.
// If implemented, CursorScope() takes precedence over CurrentPageData().
type CursorScoper interface {
	CursorScope() (func(*gorm.DB) *gorm.DB, error)
}

// OffsetPager is an optional interface for ListRequest. If implemented and Offset
// returns ok, Table.List uses offset pagination instead of cursor pagination:
// it skips offset rows, ignores the cursor entirely, and computes TotalCount so
// callers can render "page X of Y". Use this for user-driven page-number jumps.
type OffsetPager interface {
	Offset() (offset int, ok bool)
}

// BaseListRequest provides default no-op implementations of all ListRequest methods.
// Embed it in your own request struct and override only the methods you need.
// Filter rows by chaining Where/WhereIf on the Table before calling List; for
// request-driven filtering, implement the optional Scoper interface.
//
// Example:
//
//	type MyRequest struct {
//	    db.BaseListRequest[MyEntity]
//	    TenantID string
//	}
//	func (r *MyRequest) Scope() func(*gorm.DB) *gorm.DB {
//	    return func(db *gorm.DB) *gorm.DB { return db.Where("tenant_id = ?", r.TenantID) }
//	}
type BaseListRequest[T any] struct {
	pageSize int
	orderBy  string
}

func NewBaseListRequest[T any](pageSize int, orderBy string) BaseListRequest[T] {
	return BaseListRequest[T]{pageSize: pageSize, orderBy: orderBy}
}

func (b *BaseListRequest[T]) PageSize() int                              { return b.pageSize }
func (b *BaseListRequest[T]) OrderBy() string                            { return b.orderBy }
func (b *BaseListRequest[T]) CurrentPageData() ([]any, error)            { return nil, nil }
func (b *BaseListRequest[T]) CreateNextPageToken(_ []T) (*string, error) { return nil, nil }

// NewTable creates a Table for TEntity backed by a database.
func NewTable[TEntity any](db *gorm.DB) *Table[TEntity] {
	return &Table[TEntity]{gormDB: db}
}

// clone returns a shallow copy of the Table. The preloads and scopes slices are
// shared until withScope/withPreload appends, which copies first — so chained calls
// never mutate the original Table.
func (t *Table[TEntity]) clone() *Table[TEntity] {
	return &Table[TEntity]{
		gormDB:   t.gormDB,
		preloads: t.preloads,
		scopes:   t.scopes,
	}
}

// withScope returns a copy of the Table with scope appended to its query scopes.
func (t *Table[TEntity]) withScope(scope func(*gorm.DB) *gorm.DB) *Table[TEntity] {
	nt := t.clone()
	nt.scopes = append(append([]func(*gorm.DB) *gorm.DB{}, t.scopes...), scope)
	return nt
}

// Preload returns a copy of the Table that eager-loads association on every query.
// args mirrors GORM's Preload: a SQL condition string + values, or a func(*gorm.DB) *gorm.DB,
// or nothing for an unconditional preload. Use clause.Associations ("*") to preload all.
// Multiple calls accumulate; preloads do not modify the original Table.
func (t *Table[TEntity]) Preload(association string, args ...any) *Table[TEntity] {
	nt := t.clone()
	nt.preloads = append(append([]preloadEntry{}, t.preloads...), preloadEntry{query: association, args: args})
	return nt
}

// Where returns a copy of the Table with an additional WHERE clause. Calls accumulate;
// the original Table is not modified. Accepts the same formats as gorm.DB.Where.
func (t *Table[TEntity]) Where(query any, args ...any) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Where(query, args...)
	})
}

// WhereIf returns a copy of the Table with an additional WHERE clause that is applied
// only when condition is true. Calls accumulate; the original Table is not modified.
//
// Example:
//
//	results, err := table.
//	    WhereIf(req.TenantID != "", "tenant_id = ?", req.TenantID).
//	    WhereIf(req.Active, "active = true").
//	    Order("created_at DESC").
//	    ListAll(ctx)
func (t *Table[TEntity]) WhereIf(condition bool, where ...any) *Table[TEntity] {
	if !condition {
		return t
	}
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Where(where[0], where[1:]...)
	})
}

// Order returns a copy of the Table with an ORDER BY clause. Calls accumulate, so
// multiple Order calls produce ordering by the first column, then the second, etc.
// value accepts the same formats as gorm.DB.Order: a string ("created_at DESC") or
// a clause.OrderByColumn. The accumulated order applies to every read on the Table,
// including First, Last, Find, ListAll, and ListPage.
func (t *Table[TEntity]) Order(value any) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Order(value)
	})
}

// Limit returns a copy of the Table capped to n rows. A negative n cancels a previously
// applied limit. The cap applies to multi-row reads (Find, ListAll); the single-row
// getters (First, Last, Take) already limit to one row.
func (t *Table[TEntity]) Limit(n int) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Limit(n)
	})
}

// Offset returns a copy of the Table that skips the first n rows. A negative n cancels
// a previously applied offset. Pair with Order and Limit for manual offset paging;
// for page-number jumps prefer the OffsetPager path on List.
func (t *Table[TEntity]) Offset(n int) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Offset(n)
	})
}

// Select returns a copy of the Table restricted to the given columns or expressions.
// query and args accept the same formats as gorm.DB.Select: column names, a comma list,
// or a raw expression with placeholders.
func (t *Table[TEntity]) Select(query any, args ...any) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Select(query, args...)
	})
}

// Omit returns a copy of the Table that omits the given columns, wrapping GORM's Omit.
// On reads it excludes those columns from the SELECT; on Create/Save/Update it excludes
// them from the written columns. It is the complement of Select. Pass clause.Associations
// ("*") to skip auto-saving all associations on a write.
func (t *Table[TEntity]) Omit(columns ...string) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Omit(columns...)
	})
}

// Distinct returns a copy of the Table that selects distinct rows, optionally over the
// given columns. With no args it applies SELECT DISTINCT to the selected columns.
func (t *Table[TEntity]) Distinct(args ...any) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Distinct(args...)
	})
}

// Joins returns a copy of the Table with a JOIN clause. query and args accept the same
// formats as gorm.DB.Joins: a raw join expression, or an association name for a struct join.
func (t *Table[TEntity]) Joins(query string, args ...any) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Joins(query, args...)
	})
}

// Group returns a copy of the Table with a GROUP BY clause. Combine with Having and
// Select for aggregate queries.
func (t *Table[TEntity]) Group(name string) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Group(name)
	})
}

// Having returns a copy of the Table with a HAVING clause, filtering grouped rows.
// query and args accept the same formats as gorm.DB.Having.
func (t *Table[TEntity]) Having(query any, args ...any) *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Having(query, args...)
	})
}

// Unscoped returns a copy of the Table that ignores GORM's soft-delete filter, so reads
// include soft-deleted rows and Delete becomes a permanent (hard) delete.
func (t *Table[TEntity]) Unscoped() *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Unscoped()
	})
}

// LockForUpdate returns a copy of the Table whose reads emit SELECT … FOR UPDATE,
// taking a row-level pessimistic lock on the matched rows until the surrounding
// transaction commits or rolls back. Use it inside RunTx for read-modify-write
// sequences that can't be expressed as a single guarded UpdateMap, e.g.:
//
//	repo.RunTx(ctx, func(ctx context.Context) error {
//	    acct, err := t.LockForUpdate().Get(ctx, "id = ?", id) // row locked here
//	    if err != nil {
//	        return err
//	    }
//	    acct.Balance = recompute(acct.Balance)
//	    _, err = t.Update(ctx, acct)
//	    return err
//	})
//
// Engine caveat: this is a Postgres/MySQL feature. The SQLite driver silently drops
// the locking clause (the query still runs, but no lock is taken), so on SQLite the
// lock provides no protection — write safety there comes from SQLite serializing
// writers at the transaction level instead. Outside a transaction the clause is a
// no-op on every engine, since any lock would be released immediately.
//
// Prefer a single guarded UpdateMap (see its docs) when the change can be expressed
// as a SQL expression — it is atomic and portable without any locking.
func (t *Table[TEntity]) LockForUpdate() *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Clauses(clause.Locking{Strength: "UPDATE"})
	})
}

// LockForUpdateSkipLocked is LockForUpdate with SKIP LOCKED: reads emit
// SELECT … FOR UPDATE SKIP LOCKED, taking row-level locks on the matched rows
// but skipping (rather than waiting on) any row another transaction already
// locks. Use it inside a transaction so competing workers each claim a disjoint
// set of rows — the standard way to fan a queue or outbox table out across
// several concurrent consumers without any two of them grabbing the same row.
//
// Engine caveat: SKIP LOCKED is a Postgres/MySQL 8+ feature. The SQLite driver
// silently drops the locking clause, so no rows are skipped there and every
// worker sees the whole set — gate on db.Dialector.Name() and keep a
// single-consumer path for SQLite. Outside a transaction the clause is a no-op
// on every engine, since any lock would be released immediately.
func (t *Table[TEntity]) LockForUpdateSkipLocked() *Table[TEntity] {
	return t.withScope(func(db *gorm.DB) *gorm.DB {
		return db.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	})
}

// model returns a fresh zero-valued *TEntity for GORM to resolve the table and
// schema from. A new value is allocated per call rather than shared on the Table:
// map-based Updates write assigned columns (and auto-updated timestamps) back into
// the model struct via reflection, so a single shared instance would be a data race
// across concurrent writes through the same Table.
func (t *Table[TEntity]) model() *TEntity {
	return new(TEntity)
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

// CreateMany inserts items in batches of batchSize and reports how many rows were
// inserted. Use for bulk inserts. The count equals len(items) on a plain insert, so it
// is mainly a check against a conflict clause silently skipping rows.
func (t *Table[TEntity]) CreateMany(ctx context.Context, items []*TEntity, batchSize int) (int64, error) {
	res := t.db(ctx).CreateInBatches(items, batchSize)
	return res.RowsAffected, res.Error
}

// Save performs a full upsert — all fields are written — and reports how many rows were
// written. Use Update or UpdateMap for partial updates.
func (t *Table[TEntity]) Save(ctx context.Context, item *TEntity) (int64, error) {
	res := t.db(ctx).Save(item)
	return res.RowsAffected, res.Error
}

// Upsert inserts item or updates all columns if the primary key already exists, and
// reports how many rows were written. Uses a database-level ON CONFLICT clause, making
// it safe under concurrent writes. The count does not distinguish an insert from an
// update — both write one row.
func (t *Table[TEntity]) Upsert(ctx context.Context, item *TEntity) (int64, error) {
	res := t.db(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(item)
	return res.RowsAffected, res.Error
}

// Update performs a partial update — only non-zero fields in item are written — and
// reports how many rows were updated.
//
// A zero count with a nil error means no row matched: the primary key on item is absent
// from the table, or an accumulated Where scope excluded it. Treat it as the not-found
// signal rather than assuming the write landed. Callers that don't need the count can
// discard it.
//
// Only non-zero fields are written, so a field cleared to its zero value ("", 0, false)
// is skipped — use UpdateMap to write zero values explicitly.
func (t *Table[TEntity]) Update(ctx context.Context, item *TEntity) (int64, error) {
	res := t.db(ctx).Updates(item)
	return res.RowsAffected, res.Error
}

// UpdateMap applies a column→value map as a partial update and reports how many rows
// were updated. Accepts optional GORM-style where conditions (same format as Find/Count)
// to scope which rows are updated.
//
// The affected-rows count matters for capacity- or version-guarded updates whose WHERE
// clause may match no rows (e.g. an atomic counter increment bounded by a limit): a zero
// return means the guard rejected the update rather than a failure. Callers that don't
// need the count can discard it.
//
// For lock-free atomic updates, pass a gormx.Expr value so the new value is computed by
// the database in-place rather than read-then-written, and put the precondition in the
// WHERE clause. The whole thing is a single atomic statement and is portable across
// engines (SQLite included):
//
//	n, err := t.UpdateMap(ctx,
//	    map[string]any{"used": gormx.Expr("used + 1")},
//	    "id = ? AND used < capacity", id)
//	// n == 0 means the row was missing or at capacity.
func (t *Table[TEntity]) UpdateMap(ctx context.Context, values map[string]any, where ...any) (int64, error) {
	q := t.db(ctx).Model(t.model())
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	res := q.Updates(values)
	return res.RowsAffected, res.Error
}

// UpdateColumn sets a single column to value and reports how many rows were updated.
// Unlike UpdateMap it writes the raw column WITHOUT running model hooks or auto-updating
// tracked timestamps (e.g. UpdatedAt). Accepts optional GORM-style where conditions.
// Reach for it on bulk maintenance writes where hook/timestamp side effects are
// unwanted; prefer UpdateMap for ordinary updates.
func (t *Table[TEntity]) UpdateColumn(ctx context.Context, column string, value any, where ...any) (int64, error) {
	q := t.db(ctx).Model(t.model())
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	res := q.UpdateColumn(column, value)
	return res.RowsAffected, res.Error
}

// UpdateColumns applies a column→value map and reports how many rows were updated. It is
// the multi-column form of UpdateColumn: like it, the raw columns are written WITHOUT
// model hooks or timestamp auto-updates — the no-side-effects counterpart to UpdateMap.
func (t *Table[TEntity]) UpdateColumns(ctx context.Context, values map[string]any, where ...any) (int64, error) {
	q := t.db(ctx).Model(t.model())
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	res := q.UpdateColumns(values)
	return res.RowsAffected, res.Error
}

// Delete removes rows matching conditions and reports how many rows were deleted.
// Accepts the same GORM condition formats as Find.
//
// The count distinguishes "the row was already gone" from "the delete failed": a zero
// return with a nil error means nothing matched, which is how a caller detects an
// idempotent re-delete or a WHERE guard that rejected the row. Callers that don't need
// the count can discard it.
//
// On a soft-delete entity (one carrying gorm.DeletedAt) this is the number of rows
// marked deleted, not physically removed; chain Unscoped for a hard delete.
func (t *Table[TEntity]) Delete(ctx context.Context, conditions ...any) (int64, error) {
	res := t.db(ctx).Delete(t.model(), conditions...)
	return res.RowsAffected, res.Error
}

// Count returns the number of rows matching the optional where conditions.
func (t *Table[TEntity]) Count(ctx context.Context, where ...any) (int64, error) {
	var count int64
	q := t.db(ctx).Model(t.model())
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Aggregate runs an arbitrary SQL aggregate expression over the rows matching the
// optional where conditions and scans the scalar result into dest (a pointer). It is the
// general primitive behind Sum/Avg/Max/Min — reach for it directly for aggregates without
// a dedicated helper, e.g.:
//
//	var spread int
//	table.Aggregate(ctx, "MAX(price) - MIN(price)", &spread, "in_stock = ?", true)
//
// Accepts the same GORM-style where conditions as Count and composes with accumulated
// Where/WhereIf scopes. The expression is raw SQL, so when it can be NULL (e.g. over an
// empty set) wrap it in COALESCE to give dest a default rather than failing the scan.
func (t *Table[TEntity]) Aggregate(ctx context.Context, expr string, dest any, where ...any) error {
	q := t.db(ctx).Model(t.model())
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	return q.Select(expr).Scan(dest).Error
}

// Sum totals the numeric column across rows matching the optional where conditions,
// writing the result into dest (a pointer to a numeric type). No matching rows — or an
// empty table — yields zero rather than NULL (via COALESCE), so dest is always set.
func (t *Table[TEntity]) Sum(ctx context.Context, column string, dest any, where ...any) error {
	return t.Aggregate(ctx, "COALESCE(SUM("+column+"), 0)", dest, where...)
}

// Avg averages the numeric column across matching rows into dest (typically a *float64).
// An empty set yields zero rather than NULL (via COALESCE), so dest is always set.
func (t *Table[TEntity]) Avg(ctx context.Context, column string, dest any, where ...any) error {
	return t.Aggregate(ctx, "COALESCE(AVG("+column+"), 0)", dest, where...)
}

// Max writes the largest value of the numeric column across matching rows into dest.
// An empty set yields zero rather than NULL (via COALESCE), so dest is always set.
func (t *Table[TEntity]) Max(ctx context.Context, column string, dest any, where ...any) error {
	return t.Aggregate(ctx, "COALESCE(MAX("+column+"), 0)", dest, where...)
}

// Min writes the smallest value of the numeric column across matching rows into dest.
// An empty set yields zero rather than NULL (via COALESCE), so dest is always set.
func (t *Table[TEntity]) Min(ctx context.Context, column string, dest any, where ...any) error {
	return t.Aggregate(ctx, "COALESCE(MIN("+column+"), 0)", dest, where...)
}

// Pluck collects the values of a single column into dest (a pointer to a slice), one
// element per matching row with duplicates preserved. Accepts optional GORM-style where
// conditions to scope the query. Use PluckDistinct to deduplicate.
func (t *Table[TEntity]) Pluck(ctx context.Context, column string, dest any, where ...any) error {
	q := t.db(ctx).Model(t.model())
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	return q.Pluck(column, dest).Error
}

// PluckDistinct collects unique values from column into dest (a pointer to a slice).
// Accepts optional GORM-style where conditions to scope the query.
func (t *Table[TEntity]) PluckDistinct(ctx context.Context, column string, dest any, where ...any) error {
	q := t.db(ctx).Model(t.model()).Distinct(column)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	return q.Pluck(column, dest).Error
}

// Raw runs a raw SQL query and scans the result into dest — a pointer to a struct,
// slice of structs, map, or scalar. Use it as an escape hatch for queries the Table
// builder can't express (CTEs, window functions, engine-specific syntax). The SQL is
// sent verbatim, so it is engine-specific, and the Table's accumulated Where/Order/
// Preload scopes do NOT apply. It runs on the transaction in ctx when one is present
// (see RunTx).
//
//	var report []SalesRow
//	err := t.Raw(ctx, &report, "SELECT region, SUM(total) AS total FROM orders GROUP BY region")
func (t *Table[TEntity]) Raw(ctx context.Context, dest any, sql string, values ...any) error {
	return t.db(ctx).Raw(sql, values...).Scan(dest).Error
}

// Exec runs a raw SQL statement that returns no rows (INSERT/UPDATE/DELETE/DDL) and
// reports how many rows were affected. Like Raw, the SQL is engine-specific, the
// Table's builder scopes do not apply, and it participates in the ctx transaction
// when present.
func (t *Table[TEntity]) Exec(ctx context.Context, sql string, values ...any) (int64, error) {
	res := t.db(ctx).Exec(sql, values...)
	return res.RowsAffected, res.Error
}

// singleResult translates a single-row GORM result into the (nil, nil)-on-miss
// convention used by Find/First/Last/Take.
func singleResult[TEntity any](item *TEntity, err error) (*TEntity, error) {
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// First returns the first matching row ordered by primary key, or by the Table's
// accumulated Order when set, or nil if none matches (no error). Use Get when a
// missing row should be an error. Accepts GORM condition formats: primary key value,
// "col = ?", or map[string]any.
func (t *Table[TEntity]) First(ctx context.Context, conditions ...any) (*TEntity, error) {
	var item TEntity
	err := t.applyPreloads(t.db(ctx)).First(&item, conditions...).Error
	return singleResult(&item, err)
}

// Last returns the last matching row ordered by primary key, or by the Table's
// accumulated Order (reversed) when set, or nil if none matches (no error).
// Accepts the same GORM condition formats as First.
func (t *Table[TEntity]) Last(ctx context.Context, conditions ...any) (*TEntity, error) {
	var item TEntity
	err := t.applyPreloads(t.db(ctx)).Last(&item, conditions...).Error
	return singleResult(&item, err)
}

// Take returns a single matching row without any implicit ordering (the Table's
// accumulated Order still applies), or nil if none matches (no error). Use it when
// you want any one row and do not need primary-key ordering. Accepts the same GORM
// condition formats as First.
func (t *Table[TEntity]) Take(ctx context.Context, conditions ...any) (*TEntity, error) {
	var item TEntity
	err := t.applyPreloads(t.db(ctx)).Take(&item, conditions...).Error
	return singleResult(&item, err)
}

// Get is like First but treats a missing row as an error: it returns an
// errorx.ErrNotFound (a NotFound-category *errorx.Error, subject set to the entity
// type name) when no row matches. Use Get when the row must exist — e.g. loading by
// primary key behind a request handler, where absence should surface as a 404 — and
// use First when absence is an expected outcome. Other database errors are returned
// unchanged. Match with errors.Is(err, errorx.ErrNotFound).
func (t *Table[TEntity]) Get(ctx context.Context, conditions ...any) (*TEntity, error) {
	item, err := t.First(ctx, conditions...)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errorx.ErrNotFound.WithSubject(t.entityName())
	}
	return item, nil
}

// entityName returns the Go type name of TEntity, used as the subject of the
// not-found error produced by Get.
func (t *Table[TEntity]) entityName() string {
	return reflect.TypeFor[TEntity]().Name()
}

// Exists reports whether any row matches the optional where conditions (combined with
// the Table's accumulated scopes).
func (t *Table[TEntity]) Exists(ctx context.Context, where ...any) (bool, error) {
	count, err := t.Count(ctx, where...)
	if err != nil {
		return false, err
	}
	return count > 0, nil
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
// Filter and eager-load by chaining Where/WhereIf/Preload on the Table before calling;
// the request supplies page size, ordering, and the cursor. If the request implements
// Scoper, its Scope() is applied on top of those scopes. If it implements CursorScoper,
// its CursorScope() replaces CurrentPageData().
func (t *Table[TEntity]) List(ctx context.Context, req ListRequest[TEntity]) (*types.PagedResult[TEntity], error) {
	pageSize := req.PageSize()

	// Offset mode takes precedence when the request opts in via OffsetPager: it
	// bypasses the forward-only cursor and computes a total count for page jumps.
	if op, ok := any(req).(OffsetPager); ok {
		if offset, isOffset := op.Offset(); isOffset {
			return t.listByOffset(ctx, req, pageSize, offset)
		}
	}

	q := t.applyFilter(t.applyPreloads(t.db(ctx)).Order(req.OrderBy()).Limit(pageSize+1), req)
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

// applyFilter applies the request's Scoper scope, if it implements one. Simple
// row filtering is done by chaining Where/WhereIf on the Table instead, which the
// db(ctx) scopes already carry; Scoper covers request-driven filtering on top.
func (t *Table[TEntity]) applyFilter(q *gorm.DB, req ListRequest[TEntity]) *gorm.DB {
	if sr, ok := any(req).(Scoper); ok {
		return q.Scopes(sr.Scope())
	}
	return q
}

// listByOffset implements offset pagination: it counts all matching rows for
// TotalCount, then returns the page at offset. The cursor is intentionally not
// applied. Two independent queries are built from t.db(ctx) so the count and the
// page fetch do not share statement state.
func (t *Table[TEntity]) listByOffset(ctx context.Context, req ListRequest[TEntity], pageSize, offset int) (*types.PagedResult[TEntity], error) {
	var totalCount int64
	countQ := t.applyFilter(t.db(ctx).Model(t.model()), req)
	if err := countQ.Count(&totalCount).Error; err != nil {
		return nil, err
	}

	q := t.applyFilter(t.applyPreloads(t.db(ctx)).Order(req.OrderBy()).Offset(offset).Limit(pageSize), req)
	var items []TEntity
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}

	return &types.PagedResult[TEntity]{
		Items:         items,
		PageSize:      int32(pageSize),
		TotalCount:    totalCount,
		NextPageToken: nil,
	}, nil
}

// ListAll returns all matching rows. Use only when the result set is known to be small.
// Chain Where/WhereIf/Order/Preload on the Table to filter, sort, and eager-load before
// calling; without an Order the row order is database-defined.
func (t *Table[TEntity]) ListAll(ctx context.Context) ([]*TEntity, error) {
	var items []*TEntity
	if err := t.applyPreloads(t.db(ctx)).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// FindInBatches streams matching rows to fn in batches of batchSize, so large result
// sets can be processed without loading every row into memory at once, and reports how
// many rows were processed in total across all batches. Chain Where/WhereIf/Order/Preload
// on the Table to scope the query first. fn is called once per batch; returning a non-nil
// error from it stops the iteration and surfaces that error, and the count then covers
// only the batches processed up to that point. The batch slice is GORM-managed and reused
// between calls — copy out any elements you need to keep beyond the current call.
func (t *Table[TEntity]) FindInBatches(ctx context.Context, batchSize int, fn func(batch []*TEntity) error) (int64, error) {
	var batch []*TEntity
	res := t.applyPreloads(t.db(ctx)).FindInBatches(&batch, batchSize, func(_ *gorm.DB, _ int) error {
		return fn(batch)
	})
	return res.RowsAffected, res.Error
}

// Project runs the Table's query — including any chained Select/Where/Order/Group/Joins/etc
// scopes — but maps the rows into dest instead of the entity type. dest is a pointer to a
// slice of your result type. Use it for read models / DTOs that don't match the entity
// struct: pair it with Select to pick or compute the columns, which map onto dest's fields
// by name (or `gorm:"column:..."` tag). dest may also be a *[]map[string]any to receive
// rows as column→value maps when the shape isn't known ahead of time. Preloads are
// ignored — associations belong to the entity type, not the projection.
//
//	type tally struct {
//	    CampaignID uint
//	    Total      uint64
//	}
//	var rows []tally
//	err := table.Select("campaign_id, COALESCE(SUM(amount), 0) AS total").
//	    Where("is_confirmed = ?", true).
//	    Group("campaign_id").
//	    Project(ctx, &rows)
func (t *Table[TEntity]) Project(ctx context.Context, dest any) error {
	return t.db(ctx).Model(t.model()).Find(dest).Error
}

// ProjectFirst is the single-row form of Project: it maps the first matching row into dest
// (a pointer to your result struct) and reports whether a row was found. No match leaves
// dest untouched and returns (false, nil) — the missing row is not an error. Chain Order on
// the Table to control which row "first" selects; otherwise it is the entity's primary key.
func (t *Table[TEntity]) ProjectFirst(ctx context.Context, dest any) (bool, error) {
	err := t.db(ctx).Model(t.model()).First(dest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
