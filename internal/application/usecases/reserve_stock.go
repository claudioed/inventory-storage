package usecases

import (
	"context"
	"time"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// DefaultReservationTimeout is used when a use case is constructed with a
// zero Timeout.
const DefaultReservationTimeout = 30 * time.Minute

// ReserveStock revocably binds a quantity of a SKU's usable inventory to
// demand. It does not bind to one specific StockUnit permanently: the
// allocation it draws from is recorded on the Reservation so a later revoke
// returns exactly that quantity, but a future reserve is free to draw from a
// different holding.
type ReserveStock struct {
	Stock        ports.StockRepo
	Reservations ports.ReservationRepo
	Events       ports.EventPublisher
	Clock        ports.Clock
	Timeout      time.Duration
}

func (uc *ReserveStock) Execute(ctx context.Context, sku shared.SKU, qty shared.Quantity, demandRef string) (*reservation.Reservation, error) {
	if qty.Int() <= 0 {
		return nil, shared.ErrZeroQuantity
	}

	units, err := uc.Stock.FindBySKU(ctx, sku)
	if err != nil {
		return nil, err
	}

	totalUsable := shared.Quantity(0)
	for _, unit := range units {
		totalUsable = totalUsable.Add(unit.Usable())
	}
	if qty.GreaterThan(totalUsable) {
		return nil, ErrInsufficientUsable
	}

	remaining := qty
	var allocations []reservation.Allocation
	var touched []*stock.StockUnit
	for _, unit := range units {
		if remaining.Int() == 0 {
			break
		}
		usable := unit.Usable()
		if usable.Int() == 0 {
			continue
		}
		take := usable
		if !remaining.GreaterThan(usable) {
			take = remaining
		}
		if err := unit.Reserve(take); err != nil {
			return nil, err
		}
		allocations = append(allocations, reservation.Allocation{StockUnitID: unit.ID(), Quantity: take})
		touched = append(touched, unit)
		remaining, _ = remaining.Sub(take)
	}

	if remaining.Int() > 0 {
		return nil, ErrInsufficientUsable
	}

	for _, unit := range touched {
		if err := uc.Stock.Save(ctx, unit); err != nil {
			return nil, err
		}
	}

	id, err := uc.Reservations.NextID(ctx)
	if err != nil {
		return nil, err
	}

	timeout := uc.Timeout
	if timeout <= 0 {
		timeout = DefaultReservationTimeout
	}

	now := uc.Clock.Now()
	res, err := reservation.New(id, sku, qty, demandRef, allocations, now, timeout)
	if err != nil {
		return nil, err
	}

	if err := uc.Reservations.Save(ctx, res); err != nil {
		return nil, err
	}
	if err := uc.Events.Publish(ctx, shared.NewStockReserved(now, res.ID(), sku, qty, demandRef)); err != nil {
		return nil, err
	}

	return res, nil
}
