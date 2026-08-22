package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

func TestRunCycleCount_MatchesSystem_NoDiscrepancy(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	uc := &usecases.RunCycleCount{Stock: e.Stock, Events: e.Events, Clock: e.Clock}

	result, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), mustQty(t, 10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Discrepancy {
		t.Fatalf("expected no discrepancy")
	}
}

func TestRunCycleCount_Shortfall_FlagsUnlocated(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	uc := &usecases.RunCycleCount{Stock: e.Stock, Events: e.Events, Clock: e.Clock}

	result, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), mustQty(t, 4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Discrepancy {
		t.Fatalf("expected discrepancy on shortfall")
	}

	usableUC := &usecases.GetUsable{Stock: e.Stock}
	usable, _ := usableUC.Execute(context.Background(), mustSKU(t, "SKU-1"))
	if usable.Usable.Int() != 0 {
		t.Fatalf("expected usable=0 once the unit is flagged unlocated, got %d", usable.Usable.Int())
	}

	foundUnlocated := false
	for _, ev := range e.Events.Events() {
		if ev.EventName() == "ItemUnlocated" {
			foundUnlocated = true
		}
	}
	if !foundUnlocated {
		t.Fatalf("expected an ItemUnlocated event to be published")
	}
}

func TestRunCycleCount_Overage_FlagsDiscrepancyWithoutUnlocating(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 20, 10)
	uc := &usecases.RunCycleCount{Stock: e.Stock, Events: e.Events, Clock: e.Clock}

	result, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), mustQty(t, 15))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Discrepancy {
		t.Fatalf("expected discrepancy on overage")
	}

	usableUC := &usecases.GetUsable{Stock: e.Stock}
	usable, _ := usableUC.Execute(context.Background(), mustSKU(t, "SKU-1"))
	if usable.Usable.Int() != 10 {
		t.Fatalf("expected usable unchanged by overage (no auto-reconcile), got %d", usable.Usable.Int())
	}
}

func TestRunCycleCount_EmptyBin_NoDiscrepancy(t *testing.T) {
	e := newEnv()
	uc := &usecases.RunCycleCount{Stock: e.Stock, Events: e.Events, Clock: e.Clock}

	result, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Discrepancy {
		t.Fatalf("expected no discrepancy for an empty bin counted as zero")
	}
}

func TestRunCycleCount_SkipsAlreadyUnlocatedUnits(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	uc := &usecases.RunCycleCount{Stock: e.Stock, Events: e.Events, Clock: e.Clock}

	// First count flags a full shortfall, marking the unit Unlocated.
	if _, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), 0); err != nil {
		t.Fatalf("unexpected error on first count: %v", err)
	}

	// A second count should skip the already-Unlocated unit when computing
	// systemQty, so an empty count now reconciles cleanly.
	result, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), 0)
	if err != nil {
		t.Fatalf("unexpected error on second count: %v", err)
	}
	if result.Discrepancy {
		t.Fatalf("expected no discrepancy once the unlocated unit is excluded from systemQty")
	}
}

func TestRunCycleCount_StockFindByBinFails_PropagatesError(t *testing.T) {
	e := newEnv()
	stockRepo := &failingStockRepo{delegate: e.Stock, failFindByBin: true}
	uc := &usecases.RunCycleCount{Stock: stockRepo, Events: e.Events, Clock: e.Clock}

	if _, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), 0); err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestRunCycleCount_EventPublishFails_PropagatesError_NoDiscrepancy(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	uc := &usecases.RunCycleCount{Stock: e.Stock, Events: failingEvents{}, Clock: e.Clock}

	if _, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), mustQty(t, 10)); err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestRunCycleCount_EventPublishFails_Overage(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 20, 10)
	uc := &usecases.RunCycleCount{Stock: e.Stock, Events: failingEvents{}, Clock: e.Clock}

	if _, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), mustQty(t, 15)); err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestRunCycleCount_StockSaveFails_Shortfall(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	stockRepo := &failingStockRepo{delegate: e.Stock, failSave: true}
	uc := &usecases.RunCycleCount{Stock: stockRepo, Events: e.Events, Clock: e.Clock}

	if _, err := uc.Execute(context.Background(), mustBinID(t, "A-1-1"), mustQty(t, 4)); err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}
