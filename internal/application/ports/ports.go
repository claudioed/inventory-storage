// Package ports declares the outbound interfaces the application layer
// depends on. Adapters implement these; the application never imports an
// adapter package.
package ports

import (
	"context"
	"time"

	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// StockRepo persists and retrieves StockUnit aggregates.
type StockRepo interface {
	Save(ctx context.Context, unit *stock.StockUnit) error
	FindByID(ctx context.Context, id string) (*stock.StockUnit, error)
	FindBySKU(ctx context.Context, sku shared.SKU) ([]*stock.StockUnit, error)
	FindByBin(ctx context.Context, binID shared.BinId) ([]*stock.StockUnit, error)
	NextID(ctx context.Context) (string, error)
}

// LocationRepo persists and retrieves Bin aggregates.
type LocationRepo interface {
	Save(ctx context.Context, bin *location.Bin) error
	FindByID(ctx context.Context, id shared.BinId) (*location.Bin, error)
}

// ReservationRepo persists and retrieves Reservation aggregates.
type ReservationRepo interface {
	Save(ctx context.Context, r *reservation.Reservation) error
	FindByID(ctx context.Context, id string) (*reservation.Reservation, error)
	NextID(ctx context.Context) (string, error)
}

// EventPublisher publishes domain events. Adapters may log them, buffer
// them, or forward them to a broker (e.g. Kafka).
type EventPublisher interface {
	Publish(ctx context.Context, event shared.DomainEvent) error
}

// ReservationMetrics records reservation lifecycle outcomes so the business
// signal (how much demand is bound, how much comes back) is observable
// independently of HTTP traffic. Use cases treat a nil value as "not
// instrumented", so wiring it is optional.
type ReservationMetrics interface {
	ReservationCreated(ctx context.Context)
	ReservationRevoked(ctx context.Context)
}

// Clock abstracts current time so use cases and tests are deterministic.
type Clock interface {
	Now() time.Time
}
