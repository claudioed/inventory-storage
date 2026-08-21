package usecases

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// UsableInventory is the read model for GetUsable: stock immediately
// available to fulfil (on-hand minus active reservations minus
// held/damaged/unlocated).
type UsableInventory struct {
	SKU    shared.SKU
	Usable shared.Quantity
}

// GetUsable computes usable inventory for a SKU, projected from StockUnits.
type GetUsable struct {
	Stock ports.StockRepo
}

func (uc *GetUsable) Execute(ctx context.Context, sku shared.SKU) (UsableInventory, error) {
	units, err := uc.Stock.FindBySKU(ctx, sku)
	if err != nil {
		return UsableInventory{}, err
	}

	total := shared.Quantity(0)
	for _, unit := range units {
		total = total.Add(unit.Usable())
	}

	return UsableInventory{SKU: sku, Usable: total}, nil
}
