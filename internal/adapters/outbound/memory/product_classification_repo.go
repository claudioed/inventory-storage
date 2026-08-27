package memory

import (
	"context"
	"sync"

	"github.com/claudioed/inventory-storage/internal/domain/product"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// ProductClassificationRepo is an in-memory implementation of
// ports.ProductClassificationRepo.
type ProductClassificationRepo struct {
	mu              sync.RWMutex
	classifications map[shared.SKU]*product.ProductClassification
}

func NewProductClassificationRepo() *ProductClassificationRepo {
	return &ProductClassificationRepo{classifications: make(map[shared.SKU]*product.ProductClassification)}
}

func (r *ProductClassificationRepo) Save(_ context.Context, c *product.ProductClassification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classifications[c.SKU()] = c
	return nil
}

func (r *ProductClassificationRepo) FindBySKU(_ context.Context, sku shared.SKU) (*product.ProductClassification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.classifications[sku]
	if !ok {
		return nil, nil
	}
	return c, nil
}
