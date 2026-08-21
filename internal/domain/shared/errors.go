// Package shared holds value objects, domain events, and error types common
// to every aggregate in the Inventory & Storage domain.
package shared

import "errors"

var (
	ErrEmptySKU         = errors.New("sku must not be empty")
	ErrEmptyBinID       = errors.New("bin id must not be empty")
	ErrNegativeQuantity = errors.New("quantity must not be negative")
	ErrZeroQuantity     = errors.New("quantity must be greater than zero")
)
