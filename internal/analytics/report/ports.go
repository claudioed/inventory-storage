package report

import (
	"context"
	"time"
)

// ReportStore is the read side of the Inventory Flow & Accuracy data product:
// the reader process queries it to serve reports. It is read-only by contract
// — the Postgres implementation runs over a pool pinned to a read-only role.
type ReportStore interface {
	// Query returns the flow-and-accuracy rows matching q.
	Query(ctx context.Context, q ReportQuery) (FlowAccuracyReport, error)
	// FreshnessLag reports how far the read model lags real time: the age of
	// the most recently applied event. A larger lag means the projection is
	// further behind the event stream.
	FreshnessLag(ctx context.Context) (time.Duration, error)
}

// ProjectionStore is the write side of the Inventory Flow & Accuracy data
// product: the projector process applies each consumed event to it. Every
// Apply* method is idempotent on eventId — applying the same eventId twice
// records the effect once, so the at-least-once Kafka stream can be projected
// exactly once.
//
// The methods take the derivation-relevant fields already extracted from the
// analytics envelope (rather than a domain event) so this port stays free of
// any OLTP domain dependency. sku and binId are the two report dimensions;
// an event that does not carry a dimension passes "" for it.
type ProjectionStore interface {
	// ApplyStockReceived adds qty to the (sku, "", hour) row's received
	// quantity.
	ApplyStockReceived(ctx context.Context, eventId, sku string, qty int, at time.Time) error
	// ApplyItemStowed increments the (sku, binId, hour) row's stowed count.
	ApplyItemStowed(ctx context.Context, eventId, sku, binId string, at time.Time) error
	// ApplyStockPicked adds qty to the (sku, "", hour) row's picked quantity.
	ApplyStockPicked(ctx context.Context, eventId, sku string, qty int, at time.Time) error
	// ApplyStockReserved increments the (sku, "", hour) row's created-reservation
	// count.
	ApplyStockReserved(ctx context.Context, eventId, sku string, at time.Time) error
	// ApplyReservationExpired increments the (sku, "", hour) row's expired
	// count. sku is enriched onto the event by the publisher.
	ApplyReservationExpired(ctx context.Context, eventId, sku string, at time.Time) error
	// ApplyReservationRevoked increments the (sku, "", hour) row's revoked
	// count. sku is enriched onto the event by the publisher.
	ApplyReservationRevoked(ctx context.Context, eventId, sku string, at time.Time) error
	// ApplyCycleCountCompleted increments the ("", binId, hour) row's
	// cycle-count count.
	ApplyCycleCountCompleted(ctx context.Context, eventId, binId string, at time.Time) error
	// ApplyDiscrepancyDetected increments the ("", binId, hour) row's
	// discrepancy count.
	ApplyDiscrepancyDetected(ctx context.Context, eventId, binId string, at time.Time) error
	// ApplyItemUnlocated increments the (sku, binId, hour) row's unlocated
	// count.
	ApplyItemUnlocated(ctx context.Context, eventId, sku, binId string, at time.Time) error
}

// ProcessedEvents is the consumer-level idempotency gate: it records each
// analytics event id exactly once so an at-least-once redelivery is a no-op.
// It lives in the analytics read-model region (not the OLTP application ports)
// so the analytics pipeline stays independent of the OLTP layers.
type ProcessedEvents interface {
	// MarkProcessed records eventId if absent, returning true iff this call
	// newly recorded it (so the caller should process the event).
	MarkProcessed(ctx context.Context, eventId string) (bool, error)
}
