package usecases

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/product"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// ClassifyProduct registers or replaces a SKU's ProductClassification —
// SKU-level master data this service owns as source of truth (ADR 0009).
// It is idempotent by SKU: classifying the same SKU again replaces its
// prior classification rather than erroring, since re-classification is a
// legitimate operational action (e.g. an item newly designated hazmat).
type ClassifyProduct struct {
	Classifications ports.ProductClassificationRepo
	Events          ports.EventPublisher
	Clock           ports.Clock
}

// Execute validates and persists a ProductClassification for sku, and
// publishes ProductClassified.
func (uc *ClassifyProduct) Execute(ctx context.Context, sku shared.SKU, tags []product.HandlingTag, temperatureClass product.TemperatureClass) (*product.ProductClassification, error) {
	c, err := product.New(sku, tags, temperatureClass)
	if err != nil {
		return nil, err
	}

	if err := uc.Classifications.Save(ctx, c); err != nil {
		return nil, err
	}

	now := uc.Clock.Now()
	if err := uc.Events.Publish(ctx, product.NewProductClassified(c, now)); err != nil {
		return nil, err
	}

	return c, nil
}
