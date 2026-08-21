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
