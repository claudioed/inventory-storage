package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/telemetry"
)

func TestReservationMetrics_CountsByOutcome(t *testing.T) {
	reader := metric.NewManualReader()
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(metric.NewMeterProvider(metric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	metrics, err := telemetry.NewReservationMetrics()
	if err != nil {
		t.Fatalf("NewReservationMetrics: %v", err)
	}

	ctx := context.Background()
	metrics.ReservationCreated(ctx)
	metrics.ReservationCreated(ctx)
	metrics.ReservationRevoked(ctx)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	counts := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "inventory.reservations" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("inventory.reservations is a %T, want an int64 Sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				outcome, ok := point.Attributes.Value("outcome")
				if !ok {
					t.Fatal("data point has no outcome attribute")
				}
				counts[outcome.AsString()] = point.Value
			}
		}
	}

	if counts["created"] != 2 {
		t.Errorf("outcome=created count = %d, want 2", counts["created"])
	}
	if counts["revoked"] != 1 {
		t.Errorf("outcome=revoked count = %d, want 1", counts["revoked"])
	}
}

func TestReservationMetrics_RecordingNeverPanics(t *testing.T) {
	// Whatever the global provider happens to be — the no-op one before
	// Setup runs, a real one after — recording is fire-and-forget and must
	// never take the use case down with it.
	metrics, err := telemetry.NewReservationMetrics()
	if err != nil {
		t.Fatalf("NewReservationMetrics: %v", err)
	}

	metrics.ReservationCreated(context.Background())
	metrics.ReservationRevoked(context.Background())
}
