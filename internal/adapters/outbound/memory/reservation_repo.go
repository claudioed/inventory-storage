package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/claudioed/inventory-storage/internal/domain/reservation"
)

// ReservationRepo is an in-memory implementation of ports.ReservationRepo.
type ReservationRepo struct {
	mu           sync.RWMutex
	reservations map[string]*reservation.Reservation
	nextID       int
}

func NewReservationRepo() *ReservationRepo {
	return &ReservationRepo{reservations: make(map[string]*reservation.Reservation)}
}

func (r *ReservationRepo) Save(_ context.Context, res *reservation.Reservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reservations[res.ID()] = res
	return nil
}

func (r *ReservationRepo) FindByID(_ context.Context, id string) (*reservation.Reservation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res, ok := r.reservations[id]
	if !ok {
		return nil, nil
	}
	return res, nil
}

// FindByDemandRef performs a simple linear scan over every reservation held,
// filtering by demand ref. Suitable for tests/local use; the Postgres
// adapter uses an indexed query instead.
func (r *ReservationRepo) FindByDemandRef(_ context.Context, demandRef string) ([]*reservation.Reservation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var results []*reservation.Reservation
	for _, res := range r.reservations {
		if res.DemandRef() == demandRef {
			results = append(results, res)
		}
	}
	return results, nil
}

func (r *ReservationRepo) NextID(_ context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	return fmt.Sprintf("res-%d", r.nextID), nil
}
