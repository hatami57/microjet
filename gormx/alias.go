package gormx

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This file re-exports the handful of GORM types and helpers that appear in gormx's
// own public API, so callers can stay on the gormx import alone instead of also
// depending on gorm.io/gorm and gorm.io/gorm/clause directly. It is deliberately
// minimal: it covers only what gormx's signatures and docs already expose, not the
// whole GORM surface.

// DB is gorm.DB, re-exported for callers who write Where/Order/Scope closures or
// implement Scoper — their func(*gormx.DB) *gormx.DB satisfies the func(*gorm.DB)
// *gorm.DB that gormx expects, since this is an alias rather than a new type.
type DB = gorm.DB

// OrderByColumn is clause.OrderByColumn, the typed alternative to a raw "col DESC"
// string accepted by Table.Order.
type OrderByColumn = clause.OrderByColumn

// Associations is clause.Associations ("*"), the wildcard passed to Table.Preload to
// eager-load every association.
const Associations = clause.Associations

// Expr builds a raw SQL expression. Pass it as an UpdateMap value so the database
// computes the new value in-place (read-free), which keeps guarded updates atomic:
//
//	t.UpdateMap(ctx, map[string]any{"used": gormx.Expr("used + 1")}, "id = ?", id)
//
// It is gorm.Expr.
func Expr(expr string, args ...any) clause.Expr {
	return gorm.Expr(expr, args...)
}
