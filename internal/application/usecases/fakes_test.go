package usecases_test

import (
	"context"
	"errors"

	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// errFake is returned by fake adapters configured to fail, so tests can
// assert the use case propagates the exact error from its dependency.
var errFake = errors.New("fake adapter failure")

// failingEvents is an EventPublisher that always errors, used to exercise
// every use case's final Events.Publish error-return branch.
type failingEvents struct{}

func (failingEvents) Publish(context.Context, shared.DomainEvent) error { return errFake }

// failingStockRepo wraps a real StockRepo but can be configured to fail on
// specific operations, to exercise error-return branches that the in-memory
// adapter itself never triggers.
type failingStockRepo struct {
	delegate interface {
		Save(ctx context.Context, unit *stock.StockUnit) error
		FindByID(ctx context.Context, id string) (*stock.StockUnit, error)
		FindBySKU(ctx context.Context, sku shared.SKU) ([]*stock.StockUnit, error)
		FindByBin(ctx context.Context, binID shared.BinId) ([]*stock.StockUnit, error)
		NextID(ctx context.Context) (string, error)
	}
	failFindBySKU bool
	failFindByBin bool
	failFindByID  bool
	failSave      bool
	failNextID    bool
}

func (f *failingStockRepo) Save(ctx context.Context, unit *stock.StockUnit) error {
	if f.failSave {
		return errFake
	}
	return f.delegate.Save(ctx, unit)
}

func (f *failingStockRepo) FindByID(ctx context.Context, id string) (*stock.StockUnit, error) {
	if f.failFindByID {
		return nil, errFake
	}
	return f.delegate.FindByID(ctx, id)
}

func (f *failingStockRepo) FindBySKU(ctx context.Context, sku shared.SKU) ([]*stock.StockUnit, error) {
	if f.failFindBySKU {
		return nil, errFake
	}
	return f.delegate.FindBySKU(ctx, sku)
}

func (f *failingStockRepo) FindByBin(ctx context.Context, binID shared.BinId) ([]*stock.StockUnit, error) {
	if f.failFindByBin {
		return nil, errFake
	}
	return f.delegate.FindByBin(ctx, binID)
}

func (f *failingStockRepo) NextID(ctx context.Context) (string, error) {
	if f.failNextID {
		return "", errFake
	}
	return f.delegate.NextID(ctx)
}

// failingLocationRepo wraps a real LocationRepo but can be configured to
// fail on specific operations.
type failingLocationRepo struct {
	delegate interface {
		Save(ctx context.Context, bin *location.Bin) error
		FindByID(ctx context.Context, id shared.BinId) (*location.Bin, error)
	}
	failFindByID bool
	failSave     bool
}

func (f *failingLocationRepo) Save(ctx context.Context, bin *location.Bin) error {
	if f.failSave {
		return errFake
	}
	return f.delegate.Save(ctx, bin)
}

func (f *failingLocationRepo) FindByID(ctx context.Context, id shared.BinId) (*location.Bin, error) {
	if f.failFindByID {
		return nil, errFake
	}
	return f.delegate.FindByID(ctx, id)
}

// failingReservationRepo wraps a real ReservationRepo but can be configured
// to fail on specific operations.
type failingReservationRepo struct {
	delegate interface {
		Save(ctx context.Context, r *reservation.Reservation) error
		FindByID(ctx context.Context, id string) (*reservation.Reservation, error)
		NextID(ctx context.Context) (string, error)
	}
	failFindByID bool
	failSave     bool
	failNextID   bool
}

func (f *failingReservationRepo) Save(ctx context.Context, r *reservation.Reservation) error {
	if f.failSave {
		return errFake
	}
	return f.delegate.Save(ctx, r)
}

func (f *failingReservationRepo) FindByID(ctx context.Context, id string) (*reservation.Reservation, error) {
	if f.failFindByID {
		return nil, errFake
	}
	return f.delegate.FindByID(ctx, id)
}

func (f *failingReservationRepo) NextID(ctx context.Context) (string, error) {
	if f.failNextID {
		return "", errFake
	}
	return f.delegate.NextID(ctx)
}

// recordingMetrics counts the ports.ReservationMetrics calls a use case
// makes, so tests can assert the business counter moves for the right
// outcome — and only on the success path.
type recordingMetrics struct {
	created int
	revoked int
}

func (m *recordingMetrics) ReservationCreated(context.Context) { m.created++ }
func (m *recordingMetrics) ReservationRevoked(context.Context) { m.revoked++ }
