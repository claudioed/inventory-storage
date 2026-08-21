// Package location holds the Bin aggregate: a coded slot in chaotic storage.
// Any SKU may occupy any free bin, but the sum of stock held in a bin must
// never exceed its capacity.
package location

import (
	"errors"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

var (
	ErrBinFull                 = errors.New("bin is full: capacity exceeded")
	ErrInvalidCapacity         = errors.New("bin capacity must be greater than zero")
	ErrReleaseExceedsOccupancy = errors.New("cannot release more than is occupied")
)

// Bin is the aggregate root for a coded storage slot.
type Bin struct {
	id       shared.BinId
	capacity shared.Quantity
	occupied shared.Quantity
}

// NewBin constructs an empty Bin with the given capacity.
func NewBin(id shared.BinId, capacity shared.Quantity) (*Bin, error) {
	if id == "" {
		return nil, shared.ErrEmptyBinID
	}
	if capacity.Int() <= 0 {
		return nil, ErrInvalidCapacity
	}
	return &Bin{id: id, capacity: capacity, occupied: 0}, nil
}

// RehydrateBin reconstructs a Bin from persisted state without re-running
// creation invariants (used by repositories).
func RehydrateBin(id shared.BinId, capacity, occupied shared.Quantity) *Bin {
	return &Bin{id: id, capacity: capacity, occupied: occupied}
}

func (b *Bin) ID() shared.BinId          { return b.id }
func (b *Bin) Capacity() shared.Quantity { return b.capacity }
func (b *Bin) Occupied() shared.Quantity { return b.occupied }
func (b *Bin) Available() shared.Quantity {
	avail, _ := b.capacity.Sub(b.occupied)
	return avail
}
func (b *Bin) IsFull() bool { return b.occupied.GreaterThan(b.capacity) || b.occupied == b.capacity }

// Occupy reserves physical space in the bin for a stow. A full (or
// over-capacity) bin rejects the stow.
func (b *Bin) Occupy(qty shared.Quantity) error {
	if qty.Int() <= 0 {
		return shared.ErrZeroQuantity
	}
	if b.occupied.Add(qty).GreaterThan(b.capacity) {
		return ErrBinFull
	}
	b.occupied = b.occupied.Add(qty)
	return nil
}

// Release frees physical space in the bin, e.g. when stock is picked out of it.
func (b *Bin) Release(qty shared.Quantity) error {
	if qty.Int() <= 0 {
		return shared.ErrZeroQuantity
	}
	remaining, err := b.occupied.Sub(qty)
	if err != nil {
		return ErrReleaseExceedsOccupancy
	}
	b.occupied = remaining
	return nil
}
