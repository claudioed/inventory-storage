package usecases_test

import (
	"time"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/application/ports"
)

// env bundles a fresh set of in-memory adapters for each test, so tests
// never share state.
type env struct {
	Stock        *memory.StockRepo
	Locations    *memory.LocationRepo
	Reservations *memory.ReservationRepo
	Events       *events.BufferedPublisher
	Clock        *memory.FixedClock
}

func newEnv() env {
	return env{
		Stock:        memory.NewStockRepo(),
		Locations:    memory.NewLocationRepo(),
		Reservations: memory.NewReservationRepo(),
		Events:       events.NewBufferedPublisher(),
		Clock:        memory.NewFixedClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	}
}

var _ ports.Clock = (*memory.FixedClock)(nil)
