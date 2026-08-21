package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

func stowUnit(t *testing.T, e env, sku string, binID string, capacity, qty int) {
	t.Helper()
	seedBin(t, e, mustBinID(t, binID), capacity)
	stow := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}
	if _, err := stow.Execute(context.Background(), mustSKU(t, sku), mustQty(t, qty), mustBinID(t, binID)); err != nil {
		t.Fatalf("unexpected error stowing: %v", err)
	}
}

func TestReserveStock_WithinUsable_Succeeds(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	uc := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}

	res, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 6), "order-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Quantity().Int() != 6 {
		t.Fatalf("expected reserved quantity=6, got %d", res.Quantity().Int())
	}

	usableUC := &usecases.GetUsable{Stock: e.Stock}
	usable, _ := usableUC.Execute(context.Background(), mustSKU(t, "SKU-1"))
	if usable.Usable.Int() != 4 {
		t.Fatalf("expected usable=4 after reserve, got %d", usable.Usable.Int())
	}
}

// Named invariant: "reservation <= usable" — enforced end-to-end through the use case.
func TestReserveStock_ExceedsUsable_Rejected(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 5)
	uc := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 6), "order-1")
	if err != usecases.ErrInsufficientUsable {
		t.Fatalf("expected ErrInsufficientUsable, got %v", err)
	}

	usableUC := &usecases.GetUsable{Stock: e.Stock}
	usable, _ := usableUC.Execute(context.Background(), mustSKU(t, "SKU-1"))
	if usable.Usable.Int() != 5 {
		t.Fatalf("usable must be unchanged on rejected reserve, got %d", usable.Usable.Int())
	}
}

func TestReserveStock_SpansMultipleStockUnits(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 5, 5)
	stowUnit(t, e, "SKU-1", "A-1-2", 5, 5)
	uc := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}

	res, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 8), "order-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Allocations()) != 2 {
		t.Fatalf("expected allocations across 2 stock units, got %d", len(res.Allocations()))
	}
}
