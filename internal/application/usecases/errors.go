// Package usecases implements the application's use cases: one struct per
// use case, depending only on the domain and on application/ports.
package usecases

import "errors"

var (
	ErrStockUnitNotFound   = errors.New("stock unit not found")
	ErrBinNotFound         = errors.New("bin not found")
	ErrReservationNotFound = errors.New("reservation not found")
	ErrInsufficientUsable  = errors.New("requested quantity exceeds usable inventory")
)
