package gormx

import (
	"errors"

	"github.com/hatami57/microjet/core/errorx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// tracerName identifies this instrumentation in exported spans.
const tracerName = "github.com/hatami57/microjet/gormx"

// UseTracing registers OpenTelemetry callbacks on db so every create, query,
// update, delete, row, and raw operation runs inside a client span carrying
// the table, the SQL template (placeholders, never bound values), and any
// error. Spans are no-ops until a global tracer provider is installed (e.g.
// via the otelx module), so registering unconditionally is cheap. The host
// applies it automatically to driver-opened connections.
func UseTracing(db *gorm.DB) error {
	return db.Use(tracingPlugin{})
}

type tracingPlugin struct{}

func (tracingPlugin) Name() string { return "microjet:tracing" }

// registrar matches the Register method on gorm's (unexported) callback type
// returned by Before/After, letting the hooks below be wired in a loop.
type registrar interface {
	Register(name string, fn func(*gorm.DB)) error
}

func (tracingPlugin) Initialize(db *gorm.DB) error {
	dbSystem := db.Dialector.Name()
	for _, e := range []struct {
		op            string
		before, after registrar
	}{
		{"create", db.Callback().Create().Before("gorm:create"), db.Callback().Create().After("gorm:create")},
		{"query", db.Callback().Query().Before("gorm:query"), db.Callback().Query().After("gorm:query")},
		{"update", db.Callback().Update().Before("gorm:update"), db.Callback().Update().After("gorm:update")},
		{"delete", db.Callback().Delete().Before("gorm:delete"), db.Callback().Delete().After("gorm:delete")},
		{"row", db.Callback().Row().Before("gorm:row"), db.Callback().Row().After("gorm:row")},
		{"raw", db.Callback().Raw().Before("gorm:raw"), db.Callback().Raw().After("gorm:raw")},
	} {
		if err := e.before.Register("microjet:trace_before_"+e.op, traceBefore(e.op, dbSystem)); err != nil {
			return errorx.NewInternalError("gormx", "registering before callback failed", "op", e.op).WithInner(err)
		}
		if err := e.after.Register("microjet:trace_after_"+e.op, traceAfter); err != nil {
			return errorx.NewInternalError("gormx", "registering after callback failed", "op", e.op).WithInner(err)
		}
	}
	return nil
}

func traceBefore(op, dbSystem string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		ctx, _ := otel.Tracer(tracerName).Start(db.Statement.Context, "gorm."+op,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attribute.String("db.system.name", dbSystem)),
		)
		db.Statement.Context = ctx
	}
}

func traceAfter(db *gorm.DB) {
	span := trace.SpanFromContext(db.Statement.Context)
	defer span.End()
	if !span.IsRecording() {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.Int64("db.response.returned_rows", db.Statement.RowsAffected),
	}
	if table := db.Statement.Table; table != "" {
		attrs = append(attrs, attribute.String("db.collection.name", table))
	}
	// The SQL template contains placeholders, not bound values, so recording it
	// does not leak query parameters.
	if sql := db.Statement.SQL.String(); sql != "" {
		attrs = append(attrs, attribute.String("db.query.text", sql))
	}
	span.SetAttributes(attrs...)
	if err := db.Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
