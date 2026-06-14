package sqlite

import (
	"context"
	"log/slog"
	"testing"

	"github.com/hatami57/microjet/gormx"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type tracedUser struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

func TestUseTracingEmitsSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	db, err := Driver().Open(gormx.Config{Name: ":memory:"}, slog.Default())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gormx.UseTracing(db); err != nil {
		t.Fatalf("UseTracing: %v", err)
	}
	if err := db.AutoMigrate(&tracedUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	recorder.Reset() // ignore migration spans; assert on the operations below

	ctx := context.Background()
	if err := db.WithContext(ctx).Create(&tracedUser{Name: "a"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var users []tracedUser
	if err := db.WithContext(ctx).Find(&users).Error; err != nil {
		t.Fatalf("find: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range spans {
		byName[s.Name()] = s
	}
	for _, name := range []string{"gorm.create", "gorm.query"} {
		span, ok := byName[name]
		if !ok {
			t.Fatalf("missing span %q; got %v", name, spans)
		}
		if span.SpanKind() != trace.SpanKindClient {
			t.Errorf("%s: span kind = %v", name, span.SpanKind())
		}
		var sawSQL, sawTable bool
		for _, attr := range span.Attributes() {
			switch string(attr.Key) {
			case "db.query.text":
				sawSQL = attr.Value.AsString() != ""
			case "db.collection.name":
				sawTable = attr.Value.AsString() == "traced_users"
			}
		}
		if !sawSQL || !sawTable {
			t.Errorf("%s: missing sql/table attrs: %v", name, span.Attributes())
		}
	}
}

func TestUseTracingNoProviderIsNoop(t *testing.T) {
	db, err := Driver().Open(gormx.Config{Name: ":memory:"}, slog.Default())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gormx.UseTracing(db); err != nil {
		t.Fatalf("UseTracing: %v", err)
	}
	if err := db.AutoMigrate(&tracedUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&tracedUser{Name: "b"}).Error; err != nil {
		t.Fatalf("create with no-op tracer: %v", err)
	}
}
