package reservation

import (
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

func mustQty(t *testing.T, v int) shared.Quantity {
	t.Helper()
	q, err := shared.NewQuantity(v)
	if err != nil {
		t.Fatalf("unexpected error building quantity: %v", err)
	}
	return q
}

func newActive(t *testing.T) *Reservation {
	t.Helper()
	sku, _ := shared.NewSKU("SKU-1")
	allocs := []Allocation{{StockUnitID: "su-1", Quantity: mustQty(t, 5)}}
	r, err := New("r-1", sku, mustQty(t, 5), "order-1", allocs, time.Unix(0, 0), time.Hour)
	if err != nil {
		t.Fatalf("unexpected error building reservation: %v", err)
	}
	return r
}

func TestNew_RequiresAtLeastOneAllocation(t *testing.T) {
	sku, _ := shared.NewSKU("SKU-1")
	if _, err := New("r-1", sku, mustQty(t, 5), "order-1", nil, time.Unix(0, 0), time.Hour); err != ErrNoAllocations {
		t.Fatalf("expected ErrNoAllocations, got %v", err)
	}
}

func TestReservation_Revoke_Succeeds(t *testing.T) {
	r := newActive(t)
	if err := r.Revoke(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Status() != StatusRevoked {
		t.Fatalf("expected StatusRevoked, got %v", r.Status())
	}
}

// No double-consume: revoking twice is rejected.
func TestReservation_Revoke_Twice_Rejected(t *testing.T) {
	r := newActive(t)
	_ = r.Revoke()

	if err := r.Revoke(); err != ErrAlreadyResolved {
		t.Fatalf("expected ErrAlreadyResolved, got %v", err)
	}
}

func TestReservation_Confirm_AfterRevoke_Rejected(t *testing.T) {
	r := newActive(t)
	_ = r.Revoke()

	if err := r.Confirm(time.Unix(0, 0)); err != ErrAlreadyResolved {
		t.Fatalf("expected ErrAlreadyResolved, got %v", err)
	}
}

func TestReservation_Confirm_Twice_Rejected(t *testing.T) {
	r := newActive(t)
	_ = r.Confirm(time.Unix(0, 0))

	if err := r.Confirm(time.Unix(0, 0)); err != ErrAlreadyResolved {
		t.Fatalf("expected ErrAlreadyResolved, got %v", err)
	}
}

func TestReservation_Confirm_AfterExpiry_Rejected(t *testing.T) {
	r := newActive(t)
	past := r.ExpiresAt().Add(time.Second)

	if err := r.Confirm(past); err != ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestReservation_IsExpired(t *testing.T) {
	r := newActive(t)
	if r.IsExpired(r.CreatedAt()) {
		t.Fatalf("should not be expired immediately after creation")
	}
	if !r.IsExpired(r.ExpiresAt().Add(time.Second)) {
		t.Fatalf("should be expired after timeout elapses")
	}
}
