package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/claudioed/inventory-storage/internal/application/ports"
)

// meterName scopes this service's own instruments, keeping them distinct from
// the ones otelchi, otelpgx and the runtime collector register.
const meterName = "github.com/claudioed/inventory-storage"

// reservationCounterName is the business metric: how much demand is being
// bound to usable inventory, and how much of it comes back. A revoke rate
// climbing towards the create rate means physical delivery is failing
// (blocked pod, lost tote, short pick), which is exactly the failure mode
// revocable reservations exist to absorb.
const reservationCounterName = "inventory.reservations"

// outcomeKey distinguishes the two ends of a reservation's life on the single
// counter, rather than splitting it into two instruments.
const outcomeKey = attribute.Key("outcome")

const (
	outcomeCreated = "created"
	outcomeRevoked = "revoked"
)

// ReservationMetrics implements ports.ReservationMetrics against the global
// MeterProvider. Until Setup installs a real provider, the global one is a
// no-op, so recording is cheap and safe in tests and local runs.
type ReservationMetrics struct {
	counter metric.Int64Counter
}

var _ ports.ReservationMetrics = (*ReservationMetrics)(nil)

// NewReservationMetrics registers the reservation counter. It only fails if
// the instrument name is invalid, which is a programming error, not a runtime
// condition — callers that would rather run un-instrumented than not at all
// can ignore the error and pass a nil ports.ReservationMetrics instead.
func NewReservationMetrics() (*ReservationMetrics, error) {
	counter, err := otel.Meter(meterName).Int64Counter(
		reservationCounterName,
		metric.WithDescription("Reservations against usable inventory, by outcome (created or revoked)."),
		metric.WithUnit("{reservation}"),
	)
	if err != nil {
		return nil, err
	}
	return &ReservationMetrics{counter: counter}, nil
}

func (m *ReservationMetrics) ReservationCreated(ctx context.Context) {
	m.record(ctx, outcomeCreated)
}

func (m *ReservationMetrics) ReservationRevoked(ctx context.Context) {
	m.record(ctx, outcomeRevoked)
}

func (m *ReservationMetrics) record(ctx context.Context, outcome string) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcome)))
}
