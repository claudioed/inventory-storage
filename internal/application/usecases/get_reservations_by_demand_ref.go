package usecases

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
)

// GetReservationsByDemandRef looks up every Reservation aggregate ever
// created against a given demand reference (an external system's order+line
// reference, e.g. from order-management). A demandRef can have multiple
// reservations across its lifetime — one revoked, a retry that succeeded —
// so the result is a slice of domain aggregates, not a single value or a DTO
// (DTOs are the HTTP adapter's concern, not this layer's).
type GetReservationsByDemandRef struct {
	Reservations ports.ReservationRepo
}

func (uc *GetReservationsByDemandRef) Execute(ctx context.Context, demandRef string) ([]*reservation.Reservation, error) {
	return uc.Reservations.FindByDemandRef(ctx, demandRef)
}
