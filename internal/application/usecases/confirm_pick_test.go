package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

func TestConfirmPick_ConsumesReservationAndFreesBin(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	reserveUC := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}
	res, _ := reserveUC.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 6), "order-1")

	pickUC := &usecases.ConfirmPick{Stock: e.Stock, Locations: e.Locations, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}
	if err := pickUC.Execute(context.Background(), res.ID()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usableUC := &usecases.GetUsable{Stock: e.Stock}
	usable, _ := usableUC.Execute(context.Background(), mustSKU(t, "SKU-1"))
	if usable.Usable.Int() != 4 {
		t.Fatalf("expected usable=4 after pick, got %d", usable.Usable.Int())
	}

	bin, _ := e.Locations.FindByID(context.Background(), mustBinID(t, "A-1-1"))
	if bin.Occupied().Int() != 4 {
		t.Fatalf("expected bin occupied=4 after pick freed capacity, got %d", bin.Occupied().Int())
	}
}

func TestConfirmPick_AfterRevoke_Rejected(t *testing.T) {
	e := newEnv()
	stowUnit(t, e, "SKU-1", "A-1-1", 10, 10)
	reserveUC := &usecases.ReserveStock{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}
	res, _ := reserveUC.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 6), "order-1")

	revokeUC := &usecases.RevokeReservation{Stock: e.Stock, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}
	_ = revokeUC.Execute(context.Background(), res.ID())

	pickUC := &usecases.ConfirmPick{Stock: e.Stock, Locations: e.Locations, Reservations: e.Reservations, Events: e.Events, Clock: e.Clock}
	if err := pickUC.Execute(context.Background(), res.ID()); err == nil {
		t.Fatalf("expected error confirming pick on a revoked reservation")
	}
}
