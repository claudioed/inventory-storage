package location

import (
	"testing"

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

func TestNewBin_RejectsInvalidCapacity(t *testing.T) {
	binID, _ := shared.NewBinId("A-1-1")
	if _, err := NewBin(binID, mustQty(t, 0)); err != ErrInvalidCapacity {
		t.Fatalf("expected ErrInvalidCapacity, got %v", err)
	}
}

func TestBin_Occupy_WithinCapacity_Succeeds(t *testing.T) {
	binID, _ := shared.NewBinId("A-1-1")
	bin, _ := NewBin(binID, mustQty(t, 10))

	if err := bin.Occupy(mustQty(t, 7)); err != nil {
		t.Fatalf("expected stow to succeed within capacity, got %v", err)
	}
	if bin.Occupied().Int() != 7 {
		t.Fatalf("expected occupied=7, got %d", bin.Occupied().Int())
	}
}

// Named invariant: "bin-capacity rejection" — a full bin rejects stow.
func TestBin_Occupy_ExceedsCapacity_Rejected(t *testing.T) {
	binID, _ := shared.NewBinId("A-1-1")
	bin, _ := NewBin(binID, mustQty(t, 10))

	if err := bin.Occupy(mustQty(t, 10)); err != nil {
		t.Fatalf("expected initial fill to succeed, got %v", err)
	}

	err := bin.Occupy(mustQty(t, 1))
	if err != ErrBinFull {
		t.Fatalf("expected ErrBinFull when stowing into a full bin, got %v", err)
	}
	if bin.Occupied().Int() != 10 {
		t.Fatalf("occupied should be unchanged after rejected stow, got %d", bin.Occupied().Int())
	}
}

func TestBin_Occupy_PartialOverflow_Rejected(t *testing.T) {
	binID, _ := shared.NewBinId("A-1-1")
	bin, _ := NewBin(binID, mustQty(t, 10))
	_ = bin.Occupy(mustQty(t, 8))

	if err := bin.Occupy(mustQty(t, 3)); err != ErrBinFull {
		t.Fatalf("expected ErrBinFull, got %v", err)
	}
}

func TestBin_Release_ReturnsCapacity(t *testing.T) {
	binID, _ := shared.NewBinId("A-1-1")
	bin, _ := NewBin(binID, mustQty(t, 10))
	_ = bin.Occupy(mustQty(t, 6))

	if err := bin.Release(mustQty(t, 4)); err != nil {
		t.Fatalf("unexpected error releasing: %v", err)
	}
	if bin.Occupied().Int() != 2 {
		t.Fatalf("expected occupied=2, got %d", bin.Occupied().Int())
	}
}

func TestBin_Release_ExceedsOccupancy_Rejected(t *testing.T) {
	binID, _ := shared.NewBinId("A-1-1")
	bin, _ := NewBin(binID, mustQty(t, 10))
	_ = bin.Occupy(mustQty(t, 2))

	if err := bin.Release(mustQty(t, 5)); err != ErrReleaseExceedsOccupancy {
		t.Fatalf("expected ErrReleaseExceedsOccupancy, got %v", err)
	}
}
