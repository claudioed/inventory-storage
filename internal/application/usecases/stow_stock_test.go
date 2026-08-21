package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

func mustQty(t *testing.T, v int) shared.Quantity {
	t.Helper()
	q, err := shared.NewQuantity(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return q
}

func mustSKU(t *testing.T, v string) shared.SKU {
	t.Helper()
	sku, err := shared.NewSKU(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return sku
}

func mustBinID(t *testing.T, v string) shared.BinId {
	t.Helper()
	binID, err := shared.NewBinId(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return binID
}

func seedBin(t *testing.T, e env, binID shared.BinId, capacity int) {
	t.Helper()
	bin, err := location.NewBin(binID, mustQty(t, capacity))
	if err != nil {
		t.Fatalf("unexpected error seeding bin: %v", err)
	}
	if err := e.Locations.Save(context.Background(), bin); err != nil {
		t.Fatalf("unexpected error saving bin: %v", err)
	}
}

func TestStowStock_ValidItemAndLocation_Succeeds(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	unit, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Quantity().Int() != 5 {
		t.Fatalf("expected quantity=5, got %d", unit.Quantity().Int())
	}

	bin, _ := e.Locations.FindByID(context.Background(), mustBinID(t, "A-1-1"))
	if bin.Occupied().Int() != 5 {
		t.Fatalf("expected bin occupied=5, got %d", bin.Occupied().Int())
	}
}

// Named invariant: "bin-capacity rejection" — enforced end-to-end through the use case.
func TestStowStock_ExceedsBinCapacity_Rejected(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 5)
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 6), mustBinID(t, "A-1-1"))
	if err != location.ErrBinFull {
		t.Fatalf("expected ErrBinFull, got %v", err)
	}
}

func TestStowStock_UnknownBin_Rejected(t *testing.T) {
	e := newEnv()
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 1), mustBinID(t, "A-1-1"))
	if err != usecases.ErrBinNotFound {
		t.Fatalf("expected ErrBinNotFound, got %v", err)
	}
}
