package http

import (
	"errors"
	"net/http"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// statusFor maps a typed domain/application error to an HTTP status code.
func statusFor(err error) int {
	switch {
	case errors.Is(err, usecases.ErrStockUnitNotFound),
		errors.Is(err, usecases.ErrBinNotFound),
		errors.Is(err, usecases.ErrReservationNotFound):
		return http.StatusNotFound

	case errors.Is(err, shared.ErrEmptySKU),
		errors.Is(err, shared.ErrEmptyBinID),
		errors.Is(err, shared.ErrNegativeQuantity),
		errors.Is(err, shared.ErrZeroQuantity),
		errors.Is(err, stock.ErrStowRequiresItemAndLocation),
		errors.Is(err, location.ErrInvalidCapacity):
		return http.StatusBadRequest

	case errors.Is(err, location.ErrBinFull),
		errors.Is(err, location.ErrReleaseExceedsOccupancy),
		errors.Is(err, usecases.ErrInsufficientUsable),
		errors.Is(err, stock.ErrInsufficientUsable),
		errors.Is(err, stock.ErrInsufficientReserved),
		errors.Is(err, stock.ErrUnitUnlocated),
		errors.Is(err, reservation.ErrAlreadyResolved),
		errors.Is(err, reservation.ErrExpired),
		errors.Is(err, reservation.ErrNoAllocations):
		return http.StatusConflict

	default:
		return http.StatusInternalServerError
	}
}
