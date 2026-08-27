package http_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	inboundhttp "github.com/claudioed/inventory-storage/internal/adapters/inbound/http"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

// spanCapturingHandler stands in for the trace-correlating slog handler the
// composition root installs. That handler lives in the outbound telemetry
// adapter, which this inbound adapter must not import — so what is asserted
// here is the part this layer is actually responsible for: that RequestLogger
// hands its records a context carrying the request's span. Turning that
// context into trace_id/span_id attributes is tested where the handler lives.
type spanCapturingHandler struct {
	slog.Handler
	seen trace.SpanContext
}

func (h *spanCapturingHandler) Handle(ctx context.Context, record slog.Record) error {
	h.seen = trace.SpanContextFromContext(ctx)
	return h.Handler.Handle(ctx, record)
}

// tracedServer builds the router with a recording TracerProvider installed
// globally and a JSON logger writing into a buffer, so a single request can
// be inspected from both the span and the log side.
func tracedServer(t *testing.T) (http.Handler, *tracetest.SpanRecorder, *spanCapturingHandler) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	logHandler := &spanCapturingHandler{Handler: slog.NewJSONHandler(&bytes.Buffer{}, nil)}
	logger := slog.New(logHandler)

	stockRepo := memory.NewStockRepo()
	locationRepo := memory.NewLocationRepo()
	reservationRepo := memory.NewReservationRepo()
	publisher := events.NewBufferedPublisher()
	clock := memory.NewFixedClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	s := &inboundhttp.Server{
		ReceiveStock:      &usecases.ReceiveStock{Events: publisher, Clock: clock},
		StowStock:         &usecases.StowStock{Stock: stockRepo, Locations: locationRepo, Events: publisher, Clock: clock},
		ReserveStock:      &usecases.ReserveStock{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		RevokeReservation: &usecases.RevokeReservation{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		ConfirmPick:       &usecases.ConfirmPick{Stock: stockRepo, Locations: locationRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		GetUsable:         &usecases.GetUsable{Stock: stockRepo},
		RunCycleCount:     &usecases.RunCycleCount{Stock: stockRepo, Events: publisher, Clock: clock},
	}

	return inboundhttp.NewRouter(s, logger, "inventory-storage"), recorder, logHandler
}

func TestRouter_ProducesAServerSpanPerRequest(t *testing.T) {
	handler, recorder, _ := tracedServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if spans[0].SpanKind() != trace.SpanKindServer {
		t.Errorf("span kind = %v, want server", spans[0].SpanKind())
	}
}

// Span names must be the chi route *pattern*, not the raw path: naming spans
// after "/inventory/SKU-1/usable" would mint a new span name per SKU and blow
// up cardinality in the tracing backend.
func TestRouter_SpanNameIsTheRoutePatternNotTheRawPath(t *testing.T) {
	handler, recorder, _ := tracedServer(t)

	req := httptest.NewRequest(http.MethodGet, "/inventory/SKU-CARDINALITY/usable", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if name := spans[0].Name(); name != "/inventory/{sku}/usable" {
		t.Errorf("span name = %q, want the route pattern %q", name, "/inventory/{sku}/usable")
	}
}

func TestRouter_RequestLogsAreWrittenUnderTheRequestSpan(t *testing.T) {
	handler, recorder, logHandler := tracedServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	want := spans[0].SpanContext()

	if !logHandler.seen.IsValid() {
		t.Fatal("the request log was written with a context carrying no span: middleware order or the *Context logger variants are wrong")
	}
	if logHandler.seen.TraceID() != want.TraceID() {
		t.Errorf("log trace id = %s, want %s", logHandler.seen.TraceID(), want.TraceID())
	}
	if logHandler.seen.SpanID() != want.SpanID() {
		t.Errorf("log span id = %s, want %s", logHandler.seen.SpanID(), want.SpanID())
	}
}
