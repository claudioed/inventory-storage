package usecases

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// ConfirmPick consumes a reservation: reserved quantity is physically
// removed from its bin(s) and the corresponding bin capacity is released.
type ConfirmPick struct {
	Stock        ports.StockRepo
	Locations    ports.LocationRepo
	Reservations ports.ReservationRepo
	Events       ports.EventPublisher
	Clock        ports.Clock
}

func (uc *ConfirmPick) Execute(ctx context.Context, reservationID string) error {
	res, err := uc.Reservations.FindByID(ctx, reservationID)
	if err != nil {
		return err
	}
	if res == nil {
		return ErrReservationNotFound
	}

	now := uc.Clock.Now()
	if err := res.Confirm(now); err != nil {
		return err
	}

	for _, alloc := range res.Allocations() {
		unit, err := uc.Stock.FindByID(ctx, alloc.StockUnitID)
		if err != nil {
			return err
		}
		if unit == nil {
			return ErrStockUnitNotFound
		}

		binID := unit.BinID()
		if err := unit.Pick(alloc.Quantity); err != nil {
			return err
		}
		if err := uc.Stock.Save(ctx, unit); err != nil {
			return err
		}

		bin, err := uc.Locations.FindByID(ctx, binID)
		if err != nil {
			return err
		}
		if bin == nil {
			return ErrBinNotFound
		}
		if err := bin.Release(alloc.Quantity); err != nil {
			return err
		}
		if err := uc.Locations.Save(ctx, bin); err != nil {
			return err
		}
	}

	if err := uc.Reservations.Save(ctx, res); err != nil {
		return err
	}

	return uc.Events.Publish(ctx, shared.NewStockPicked(now, res.ID(), res.SKU(), res.Quantity()))
}
