package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

var base = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// harness builds Deps over in-memory repos, seeding stock directly and
// reservations through the real ReserveStock use case, with a FixedClock so
// timing is deterministic.
type harness struct {
	deps     Deps
	stock    *memory.StockRepo
	reserves *memory.ReservationRepo
	reserve  *usecases.ReserveStock
	clock    *memory.FixedClock
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	stockRepo := memory.NewStockRepo()
	reservationRepo := memory.NewReservationRepo()
	publisher := events.NewBufferedPublisher()
	clock := memory.NewFixedClock(base)

	return &harness{
		deps: Deps{
			GetUsable:         &usecases.GetUsable{Stock: stockRepo},
			RevokeReservation: &usecases.RevokeReservation{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
			Stock:             stockRepo,
		},
		stock:    stockRepo,
		reserves: reservationRepo,
		reserve:  &usecases.ReserveStock{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		clock:    clock,
	}
}

// seedStock stows a StockUnit for sku@bin with the given quantity directly
// into the repo (bypassing the stow use case, which is not part of this
// adapter's surface).
func (h *harness) seedStock(t *testing.T, id, sku, bin string, qty int) {
	t.Helper()
	q, err := shared.NewPositiveQuantity(qty)
	if err != nil {
		t.Fatalf("seedStock quantity: %v", err)
	}
	unit, err := stock.NewStockUnit(id, shared.SKU(sku), shared.BinId(bin), q)
	if err != nil {
		t.Fatalf("seedStock unit: %v", err)
	}
	if err := h.stock.Save(context.Background(), unit); err != nil {
		t.Fatalf("seedStock save: %v", err)
	}
}

// seedReservation reserves qty of sku via the real use case and returns the
// reservation id, so a revoke test operates on genuine domain state.
func (h *harness) seedReservation(t *testing.T, sku string, qty int) string {
	t.Helper()
	q, err := shared.NewPositiveQuantity(qty)
	if err != nil {
		t.Fatalf("seedReservation quantity: %v", err)
	}
	res, err := h.reserve.Execute(context.Background(), shared.SKU(sku), q, "demand-1")
	if err != nil {
		t.Fatalf("seedReservation reserve: %v", err)
	}
	return res.ID()
}

func TestCheckAvailability(t *testing.T) {
	h := newHarness(t)
	// Two bins holding SKU-A: 10 + 5 usable. SKU-B: 3.
	h.seedStock(t, "su-1", "SKU-A", "BIN-1", 10)
	h.seedStock(t, "su-2", "SKU-A", "BIN-2", 5)
	h.seedStock(t, "su-3", "SKU-B", "BIN-3", 3)

	tests := []struct {
		name       string
		sku        string
		wantUsable int
		wantErr    bool
	}{
		{"sku-a aggregates across bins", "SKU-A", 15, false},
		{"sku-b single bin", "SKU-B", 3, false},
		{"unknown sku is zero, not error", "SKU-Z", 0, false},
		{"empty sku rejected", "", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := h.deps.checkAvailability(context.Background(), checkAvailabilityInput{SKU: tc.sku})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Usable != tc.wantUsable {
				t.Fatalf("usable = %d, want %d", out.Usable, tc.wantUsable)
			}
		})
	}
}

func TestCheckAvailabilityReflectsReservations(t *testing.T) {
	h := newHarness(t)
	h.seedStock(t, "su-1", "SKU-A", "BIN-1", 10)

	before, err := h.deps.checkAvailability(context.Background(), checkAvailabilityInput{SKU: "SKU-A"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if before.Usable != 10 {
		t.Fatalf("usable before reserve = %d, want 10", before.Usable)
	}

	h.seedReservation(t, "SKU-A", 4)

	after, err := h.deps.checkAvailability(context.Background(), checkAvailabilityInput{SKU: "SKU-A"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if after.Usable != 6 {
		t.Fatalf("usable after reserving 4 = %d, want 6", after.Usable)
	}
}

func TestGetBinOccupancy(t *testing.T) {
	h := newHarness(t)
	// BIN-1 holds two SKUs; reserve 3 of SKU-A to exercise usable vs reserved.
	h.seedStock(t, "su-1", "SKU-A", "BIN-1", 10)
	h.seedStock(t, "su-2", "SKU-B", "BIN-1", 5)
	h.seedReservation(t, "SKU-A", 3)

	out, err := h.deps.getBinOccupancy(context.Background(), binOccupancyInput{BinId: "BIN-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.UnitCount != 2 {
		t.Fatalf("unitCount = %d, want 2", out.UnitCount)
	}
	if out.OnHand != 15 {
		t.Fatalf("onHand = %d, want 15", out.OnHand)
	}
	if out.Reserved != 3 {
		t.Fatalf("reserved = %d, want 3", out.Reserved)
	}
	if out.Usable != 12 {
		t.Fatalf("usable = %d, want 12", out.Usable)
	}
	if len(out.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(out.Lines))
	}
}

func TestGetBinOccupancyEmptyAndInvalid(t *testing.T) {
	h := newHarness(t)

	// An empty bin is a valid, zero-occupancy answer, not an error.
	empty, err := h.deps.getBinOccupancy(context.Background(), binOccupancyInput{BinId: "BIN-EMPTY"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if empty.UnitCount != 0 || empty.OnHand != 0 || len(empty.Lines) != 0 {
		t.Fatalf("empty bin = %+v, want zero occupancy", empty)
	}

	// An empty bin id is rejected (untrusted model input).
	if _, err := h.deps.getBinOccupancy(context.Background(), binOccupancyInput{BinId: ""}); err == nil {
		t.Fatal("empty bin id must be rejected")
	}
}

func TestRevokeReservation(t *testing.T) {
	ctx := context.Background()

	t.Run("revoke returns quantity to usable", func(t *testing.T) {
		h := newHarness(t)
		h.seedStock(t, "su-1", "SKU-A", "BIN-1", 10)
		resID := h.seedReservation(t, "SKU-A", 4)

		// Usable is 6 while reserved.
		mid, _ := h.deps.checkAvailability(ctx, checkAvailabilityInput{SKU: "SKU-A"})
		if mid.Usable != 6 {
			t.Fatalf("usable while reserved = %d, want 6", mid.Usable)
		}

		out, err := h.deps.revokeReservation(ctx, revokeReservationInput{ReservationId: resID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.Revoked || out.ReservationId != resID {
			t.Fatalf("unexpected output: %+v", out)
		}

		// Usable is back to 10 after revoke.
		after, _ := h.deps.checkAvailability(ctx, checkAvailabilityInput{SKU: "SKU-A"})
		if after.Usable != 10 {
			t.Fatalf("usable after revoke = %d, want 10", after.Usable)
		}
	})

	t.Run("double revoke is rejected", func(t *testing.T) {
		h := newHarness(t)
		h.seedStock(t, "su-1", "SKU-A", "BIN-1", 10)
		resID := h.seedReservation(t, "SKU-A", 4)
		if _, err := h.deps.revokeReservation(ctx, revokeReservationInput{ReservationId: resID}); err != nil {
			t.Fatalf("first revoke failed: %v", err)
		}
		if _, err := h.deps.revokeReservation(ctx, revokeReservationInput{ReservationId: resID}); err == nil {
			t.Fatal("second revoke must be rejected")
		}
	})

	t.Run("missing id is rejected", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.deps.revokeReservation(ctx, revokeReservationInput{ReservationId: ""}); err == nil {
			t.Fatal("empty reservationId must be rejected")
		}
	})

	t.Run("unknown reservation is rejected", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.deps.revokeReservation(ctx, revokeReservationInput{ReservationId: "does-not-exist"}); err == nil {
			t.Fatal("revoking an unknown reservation must error")
		}
	})
}
