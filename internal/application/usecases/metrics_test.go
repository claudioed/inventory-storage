package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

var _ ports.ReservationMetrics = (*recordingMetrics)(nil)

func TestReserveStock_RecordsTheCreatedOutcome(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	metrics := &recordingMetrics{}
	uc := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock, Metrics: metrics}

	if _, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 4), "order-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.created != 1 {
		t.Errorf("created count = %d, want 1", metrics.created)
	}
	if metrics.revoked != 0 {
		t.Errorf("revoked count = %d, want 0", metrics.revoked)
	}
}

// Demand that was never bound must not show up as bound: the counter only
// moves once the reservation is durably saved and published.
func TestReserveStock_DoesNotRecordAFailedReservation(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 1)
	metrics := &recordingMetrics{}
	uc := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock, Metrics: metrics}

	if _, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 99), "order-1"); err == nil {
		t.Fatal("expected ReserveStock to reject a quantity above usable")
	}

	if metrics.created != 0 {
		t.Errorf("created count = %d, want 0", metrics.created)
	}
}

func TestRevokeReservation_RecordsTheRevokedOutcome(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	metrics := &recordingMetrics{}

	reserve := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock, Metrics: metrics}
	res, err := reserve.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 4), "order-1")
	if err != nil {
		t.Fatalf("unexpected error reserving: %v", err)
	}

	revoke := &usecases.RevokeReservation{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock, Metrics: metrics}
	if err := revoke.Execute(context.Background(), res.ID()); err != nil {
		t.Fatalf("unexpected error revoking: %v", err)
	}

	if metrics.created != 1 {
		t.Errorf("created count = %d, want 1", metrics.created)
	}
	if metrics.revoked != 1 {
		t.Errorf("revoked count = %d, want 1", metrics.revoked)
	}
}

func TestRevokeReservation_DoesNotRecordAFailedRevoke(t *testing.T) {
	e := newEnv()
	metrics := &recordingMetrics{}
	revoke := &usecases.RevokeReservation{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock, Metrics: metrics}

	if err := revoke.Execute(context.Background(), "no-such-reservation"); err == nil {
		t.Fatal("expected RevokeReservation to reject an unknown reservation")
	}

	if metrics.revoked != 0 {
		t.Errorf("revoked count = %d, want 0", metrics.revoked)
	}
}

// The Metrics port is optional: every other test in this package leaves it
// nil, and this pins that as intended behaviour rather than an accident.
func TestReserveAndRevoke_WithoutMetricsWired(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)

	reserve := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}
	res, err := reserve.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 4), "order-1")
	if err != nil {
		t.Fatalf("unexpected error reserving with a nil Metrics: %v", err)
	}

	revoke := &usecases.RevokeReservation{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}
	if err := revoke.Execute(context.Background(), res.ID()); err != nil {
		t.Fatalf("unexpected error revoking with a nil Metrics: %v", err)
	}
}
