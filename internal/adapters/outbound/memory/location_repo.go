package memory

import (
	"context"
	"sync"

	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// LocationRepo is an in-memory implementation of ports.LocationRepo.
type LocationRepo struct {
	mu   sync.RWMutex
	bins map[shared.BinId]*location.Bin
}

func NewLocationRepo() *LocationRepo {
	return &LocationRepo{bins: make(map[shared.BinId]*location.Bin)}
}

func (r *LocationRepo) Save(_ context.Context, bin *location.Bin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bins[bin.ID()] = bin
	return nil
}

func (r *LocationRepo) FindByID(_ context.Context, id shared.BinId) (*location.Bin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bin, ok := r.bins[id]
	if !ok {
		return nil, nil
	}
	return bin, nil
}
