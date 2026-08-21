package usecases

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// StowStock places received stock into a bin. It requires both an
// item-scan (SKU) and a location-scan (BinId), and it respects bin capacity.
type StowStock struct {
	Stock     ports.StockRepo
	Locations ports.LocationRepo
	Events    ports.EventPublisher
	Clock     ports.Clock
}

func (uc *StowStock) Execute(ctx context.Context, sku shared.SKU, qty shared.Quantity, binID shared.BinId) (*stock.StockUnit, error) {
	bin, err := uc.Locations.FindByID(ctx, binID)
	if err != nil {
		return nil, err
	}
	if bin == nil {
		return nil, ErrBinNotFound
	}

	id, err := uc.Stock.NextID(ctx)
	if err != nil {
		return nil, err
	}

	unit, err := stock.NewStockUnit(id, sku, binID, qty)
	if err != nil {
		return nil, err
	}

	if err := bin.Occupy(qty); err != nil {
		return nil, err
	}

	if err := uc.Locations.Save(ctx, bin); err != nil {
		return nil, err
	}
	if err := uc.Stock.Save(ctx, unit); err != nil {
		return nil, err
	}

	now := uc.Clock.Now()
	if err := uc.Events.Publish(ctx, shared.NewItemStowed(now, sku, binID, qty)); err != nil {
		return nil, err
	}
	if err := uc.Events.Publish(ctx, shared.NewLocationRecorded(now, unit.ID(), binID)); err != nil {
		return nil, err
	}

	return unit, nil
}
