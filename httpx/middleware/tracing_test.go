package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// withTestTracer installs a recording tracer provider and W3C propagator for
// the duration of the test, returning the span recorder.
func withTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prevProvider := otel.GetTracerProvider()
	prevPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevProvider)
		otel.SetTextMapPropagator(prevPropagator)
	})
	return recorder
}

func newTracedRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Tracing())
	r.GET("/items/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/boom", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	return r
}

func TestTracingCreatesServerSpanNamedByRoute(t *testing.T) {
	recorder := withTestTracer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/items/42", nil)
	newTracedRouter().ServeHTTP(w, req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	if span.Name() != "GET /items/:id" {
		t.Errorf("span name = %q", span.Name())
	}
	if span.SpanKind() != trace.SpanKindServer {
		t.Errorf("span kind = %v", span.SpanKind())
	}
	var sawStatus bool
	for _, attr := range span.Attributes() {
		if string(attr.Key) == "http.response.status_code" && attr.Value.AsInt64() == http.StatusOK {
			sawStatus = true
		}
	}
	if !sawStatus {
		t.Errorf("missing status attribute; attrs = %v", span.Attributes())
	}
}

func TestTracingContinuesRemoteTrace(t *testing.T) {
	recorder := withTestTracer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/items/1", nil)
	req.Header.Set("traceparent", "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01")
	newTracedRouter().ServeHTTP(w, req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].SpanContext().TraceID().String(); got != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("trace id = %s, want inherited remote trace id", got)
	}
	if got := spans[0].Parent().SpanID().String(); got != "0102030405060708" {
		t.Errorf("parent span id = %s, want remote parent", got)
	}
}

func TestTracingMarksServerErrors(t *testing.T) {
	recorder := withTestTracer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	newTracedRouter().ServeHTTP(w, req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code.String() != "Error" {
		t.Errorf("status = %v, want Error", spans[0].Status())
	}
}
