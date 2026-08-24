// Package mcp is the inbound Model Context Protocol adapter: it exposes this
// bounded context to the AI ecosystem as a second driving adapter over the
// same application-layer use cases the HTTP adapter uses. It is built on the
// official MCP Go SDK and served over Streamable HTTP.
//
// Per ADR-0008 and the MCP governance charter, this package depends inward on
// the application layer (use cases and ports) and the domain only — never on
// an outbound adapter. The composition root (cmd/mcp) wires concrete
// repositories into the use cases and query port. Tool handlers call use
// cases; domain structs never leak across the tool boundary.
package mcp

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// StockQueries is the narrow read-only port this adapter needs beyond the
// GetUsable use case, to answer the bin-occupancy diagnostic without reaching
// into an outbound adapter. It is satisfied by the same StockRepo the use
// cases use (its FindByBin method); the composition root injects that
// implementation. Keeping it as a port here preserves the dependency rule:
// the adapter depends on an interface, not on outbound/postgres or
// outbound/memory.
type StockQueries interface {
	// FindByBin returns every StockUnit currently recorded in the given bin.
	FindByBin(ctx context.Context, binID shared.BinId) ([]*stock.StockUnit, error)
}

// binOccupancy is the structured result of the get_bin_occupancy tool: a
// bin's total on-hand, reserved, and usable quantities plus a per-SKU
// breakdown. It is a tool-boundary DTO, not a domain type.
type binOccupancy struct {
	BinId     string             `json:"binId"`
	UnitCount int                `json:"unitCount"`
	OnHand    int                `json:"onHand"`
	Reserved  int                `json:"reserved"`
	Usable    int                `json:"usable"`
	Lines     []binOccupancyLine `json:"lines"`
}

// binOccupancyLine is one StockUnit's contribution to a bin's occupancy.
type binOccupancyLine struct {
	StockUnitId string `json:"stockUnitId"`
	SKU         string `json:"sku"`
	OnHand      int    `json:"onHand"`
	Reserved    int    `json:"reserved"`
	Usable      int    `json:"usable"`
	State       string `json:"state"`
}

// toBinOccupancy folds a bin's StockUnits into the tool-boundary DTO. Nothing
// but this file's DTOs crosses the tool boundary.
func toBinOccupancy(binID string, units []*stock.StockUnit) binOccupancy {
	occ := binOccupancy{BinId: binID, UnitCount: len(units), Lines: make([]binOccupancyLine, 0, len(units))}
	for _, u := range units {
		onHand := u.Quantity().Int()
		reserved := u.Reserved().Int()
		usable := u.Usable().Int()
		occ.OnHand += onHand
		occ.Reserved += reserved
		occ.Usable += usable
		occ.Lines = append(occ.Lines, binOccupancyLine{
			StockUnitId: u.ID(),
			SKU:         u.SKU().String(),
			OnHand:      onHand,
			Reserved:    reserved,
			Usable:      usable,
			State:       string(u.State()),
		})
	}
	return occ
}
