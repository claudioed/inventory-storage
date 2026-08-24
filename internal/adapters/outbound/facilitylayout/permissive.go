// Package facilitylayout provides outbound LocationClassificationLookup
// implementations: an HTTP client that calls facility-layout's
// location-classification endpoint, and a permissive no-op used by
// default so existing tests, CI and deployments are unaffected (mirrors
// the EVENT_PUBLISHER=kafka|log pattern in outbound/kafka vs
// outbound/events — see ADR 0009).
package facilitylayout

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/domain/product"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// PermissiveLookup is the default LocationClassificationLookup: it never
// contacts facility-layout and always reports Known=false, which
// StowStock's placement check treats as "no constraint info available,
// permit the stow" (fail-open). Selected via LOCATION_LOOKUP_MODE
// (default "permissive"), so existing tests, CI and deployments that do
// not set the env var see identical behaviour to before this feature
// existed.
type PermissiveLookup struct{}

// NewPermissiveLookup constructs a PermissiveLookup.
func NewPermissiveLookup() *PermissiveLookup {
	return &PermissiveLookup{}
}

func (PermissiveLookup) GetSlotAttributes(_ context.Context, _ shared.BinId) (product.SlotAttributes, error) {
	return product.SlotAttributes{Known: false}, nil
}
