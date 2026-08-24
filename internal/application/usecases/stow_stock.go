package usecases

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// StowStock places received stock into a bin. It requires both an
// item-scan (SKU) and a location-scan (BinId), and it respects bin
// capacity.
//
// If the SKU carries a registered ProductClassification, StowStock also
// enforces hazmat/temperature placement rules by consulting the
// Classifications repo and the LocationClassificationLookup port (a
// synchronous cross-context read from facility-layout — see ADR 0009):
//   - a Hazmat SKU may only be stowed into a hazmat-rated bin;
//   - a TemperatureSensitive SKU may only be stowed into a bin whose zone
//     temperature class matches the SKU's required TemperatureClass.
//
// Both Classifications and LocationLookup are nil-safe: a StowStock built
// without them (as every pre-existing test in this package does) behaves
// exactly as before — permissive, no placement check at all. This is the
// same "additive, does not alter existing behaviour" discipline documented
// in the Kafka/EVENT_PUBLISHER pattern.
type StowStock struct {
	Stock           ports.StockRepo
	Locations       ports.LocationRepo
	Events          ports.EventPublisher
	Clock           ports.Clock
	Classifications ports.ProductClassificationRepo
	LocationLookup  ports.LocationClassificationLookup
}

func (uc *StowStock) Execute(ctx context.Context, sku shared.SKU, qty shared.Quantity, binID shared.BinId) (*stock.StockUnit, error) {
	bin, err := uc.Locations.FindByID(ctx, binID)
	if err != nil {
		return nil, err
	}
	if bin == nil {
		return nil, ErrBinNotFound
	}

	if err := uc.checkPlacement(ctx, sku, binID); err != nil {
		return nil, err
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

// checkPlacement enforces hazmat/temperature placement rules for sku
// against binID, when both a classification repo and a lookup port are
// wired and the SKU is actually classified with a rule-relevant tag.
//
// A SKU with no registered classification, or one whose classification
// carries neither Hazmat nor TemperatureSensitive, is never blocked here —
// including when the lookup itself is unavailable (fail-closed applies
// only to classified SKUs; see ADR 0009).
func (uc *StowStock) checkPlacement(ctx context.Context, sku shared.SKU, binID shared.BinId) error {
	if uc.Classifications == nil || uc.LocationLookup == nil {
		return nil
	}

	classification, err := uc.Classifications.FindBySKU(ctx, sku)
	if err != nil {
		return err
	}
	if classification == nil {
		return nil
	}

	requiresHazmatZone := classification.IsHazmat()
	requiresTemperatureClass := classification.IsTemperatureSensitive()
	if !requiresHazmatZone && !requiresTemperatureClass {
		return nil
	}

	attrs, err := uc.LocationLookup.GetSlotAttributes(ctx, binID)
	if err != nil {
		// Fail-closed, but only because this SKU is classified as Hazmat
		// or TemperatureSensitive: an unavailable lookup for an
		// unclassified SKU never reaches this branch.
		return ErrLocationClassificationUnavailable
	}
	if !attrs.Known {
		// Fail-open: no constraint info for this bin, permit the stow.
		return nil
	}

	if requiresHazmatZone && !attrs.Hazmat {
		return ErrHazmatZoneRequired
	}
	if requiresTemperatureClass && attrs.TemperatureClass != classification.TemperatureClass() {
		return ErrTemperatureClassMismatch
	}

	return nil
}
