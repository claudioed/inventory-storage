package usecases

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// RevokeReservation cancels a reservation and returns its quantity to
// usable, so a failed physical delivery never strands an order.
type RevokeReservation struct {
	Stock        ports.StockRepo
	Reservations ports.ReservationRepo
	Events       ports.EventPublisher
	Clock        ports.Clock
}

func (uc *RevokeReservation) Execute(ctx context.Context, reservationID string) error {
	res, err := uc.Reservations.FindByID(ctx, reservationID)
	if err != nil {
		return err
	}
	if res == nil {
		return ErrReservationNotFound
	}

	if err := res.Revoke(); err != nil {
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
		if err := unit.ReleaseReservation(alloc.Quantity); err != nil {
			return err
		}
		if err := uc.Stock.Save(ctx, unit); err != nil {
			return err
		}
	}

	if err := uc.Reservations.Save(ctx, res); err != nil {
		return err
	}

	return uc.Events.Publish(ctx, shared.NewReservationRevoked(uc.Clock.Now(), res.ID()))
}
