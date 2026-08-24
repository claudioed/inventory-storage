// Package product holds the ProductClassification aggregate: SKU-level
// master data describing how an item must be handled (hazmat, fragile,
// temperature-sensitive, oversized, high-value), independent of any
// StockUnit or bin. This service owns this classification as its source of
// truth — it is not derived from, or shared with, any other bounded
// context.
//
// TemperatureClass here is a deliberate, small duplication of the
// facility-layout bounded context's own TemperatureClass concept (see ADR
// 0009). Bounded contexts do not share Go types across repository
// boundaries; each side names and validates the concept in its own
// ubiquitous language, translating at the integration edge instead.
package product

import (
	"errors"
	"time"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

var (
	// ErrUnknownHandlingTag is returned when a tag value is not one of the
	// closed enum members.
	ErrUnknownHandlingTag = errors.New("unknown handling tag")
	// ErrUnknownTemperatureClass is returned when a temperature class value
	// is not one of the closed enum members.
	ErrUnknownTemperatureClass = errors.New("unknown temperature class")
	// ErrNoHandlingTags is returned when a classification is constructed
	// with no handling tags at all — a classification must say something.
	ErrNoHandlingTags = errors.New("classification requires at least one handling tag")
	// ErrTemperatureClassRequired is returned when HandlingTags contains
	// TemperatureSensitive but no valid TemperatureClass was supplied.
	ErrTemperatureClassRequired = errors.New("temperature-sensitive classification requires a temperature class")
	// ErrTemperatureClassNotApplicable is returned when a TemperatureClass
	// is supplied but HandlingTags does not contain TemperatureSensitive —
	// the value would be meaningless and is rejected rather than ignored.
	ErrTemperatureClassNotApplicable = errors.New("temperature class is only meaningful when the temperature-sensitive tag is present")
	// ErrDuplicateHandlingTag is returned when the same tag appears more
	// than once in the input list — HandlingTags is a set, not a list.
	ErrDuplicateHandlingTag = errors.New("duplicate handling tag")
)

// HandlingTag is one member of the closed set of ways a SKU may require
// special handling. Unlike facility-layout's Zone.LocationType (an open
// tag list), this is deliberately closed: the taxonomy is fixed business
// vocabulary, not an extensible attribute bag.
type HandlingTag string

const (
	Hazmat               HandlingTag = "Hazmat"
	Fragile              HandlingTag = "Fragile"
	TemperatureSensitive HandlingTag = "TemperatureSensitive"
	Oversized            HandlingTag = "Oversized"
	HighValue            HandlingTag = "HighValue"
)

// ParseHandlingTag validates a string against the closed HandlingTag enum.
func ParseHandlingTag(value string) (HandlingTag, error) {
	switch HandlingTag(value) {
	case Hazmat, Fragile, TemperatureSensitive, Oversized, HighValue:
		return HandlingTag(value), nil
	default:
		return "", ErrUnknownHandlingTag
	}
}

// TemperatureClass is the storage temperature band a TemperatureSensitive
// SKU requires. This mirrors facility-layout's Zone.TemperatureClass value
// by name and meaning, but is a distinct type owned by this context — see
// the package doc comment and ADR 0009.
type TemperatureClass string

const (
	Ambient TemperatureClass = "Ambient"
	Chilled TemperatureClass = "Chilled"
	Frozen  TemperatureClass = "Frozen"
)

// ParseTemperatureClass validates a string against the closed
// TemperatureClass enum.
func ParseTemperatureClass(value string) (TemperatureClass, error) {
	switch TemperatureClass(value) {
	case Ambient, Chilled, Frozen:
		return TemperatureClass(value), nil
	default:
		return "", ErrUnknownTemperatureClass
	}
}

// ProductClassification is the aggregate root for a SKU's handling
// classification — master data, independent of any StockUnit or bin.
//
// Invariant: TemperatureClass is required and non-empty if and only if
// HandlingTags contains TemperatureSensitive. Absence of the
// TemperatureSensitive tag means TemperatureClass must be empty.
type ProductClassification struct {
	sku              shared.SKU
	handlingTags     map[HandlingTag]struct{}
	temperatureClass TemperatureClass
}

// New constructs a ProductClassification for a SKU, validating the closed
// HandlingTag enum, rejecting duplicates, and enforcing the
// TemperatureSensitive/TemperatureClass invariant.
func New(sku shared.SKU, tags []HandlingTag, temperatureClass TemperatureClass) (*ProductClassification, error) {
	if sku == "" {
		return nil, shared.ErrEmptySKU
	}
	if len(tags) == 0 {
		return nil, ErrNoHandlingTags
	}

	set := make(map[HandlingTag]struct{}, len(tags))
	for _, tag := range tags {
		if _, err := ParseHandlingTag(string(tag)); err != nil {
			return nil, err
		}
		if _, exists := set[tag]; exists {
			return nil, ErrDuplicateHandlingTag
		}
		set[tag] = struct{}{}
	}

	_, tempSensitive := set[TemperatureSensitive]
	if tempSensitive {
		if temperatureClass == "" {
			return nil, ErrTemperatureClassRequired
		}
		if _, err := ParseTemperatureClass(string(temperatureClass)); err != nil {
			return nil, err
		}
	} else if temperatureClass != "" {
		return nil, ErrTemperatureClassNotApplicable
	}

	return &ProductClassification{
		sku:              sku,
		handlingTags:     set,
		temperatureClass: temperatureClass,
	}, nil
}

// Rehydrate reconstructs a ProductClassification from persisted state
// without re-running construction invariants (used by repositories).
func Rehydrate(sku shared.SKU, tags []HandlingTag, temperatureClass TemperatureClass) *ProductClassification {
	set := make(map[HandlingTag]struct{}, len(tags))
	for _, tag := range tags {
		set[tag] = struct{}{}
	}
	return &ProductClassification{sku: sku, handlingTags: set, temperatureClass: temperatureClass}
}

// SKU returns the classified SKU.
func (c *ProductClassification) SKU() shared.SKU { return c.sku }

// TemperatureClass returns the required temperature band, or "" if the SKU
// is not TemperatureSensitive.
func (c *ProductClassification) TemperatureClass() TemperatureClass { return c.temperatureClass }

// HandlingTags returns the classification's tags as a stable-ordered slice
// (enum declaration order), so callers get deterministic output without
// holding a reference to the internal set representation.
func (c *ProductClassification) HandlingTags() []HandlingTag {
	ordered := []HandlingTag{Hazmat, Fragile, TemperatureSensitive, Oversized, HighValue}
	tags := make([]HandlingTag, 0, len(c.handlingTags))
	for _, tag := range ordered {
		if _, ok := c.handlingTags[tag]; ok {
			tags = append(tags, tag)
		}
	}
	return tags
}

// HasTag reports whether the classification carries the given tag.
func (c *ProductClassification) HasTag(tag HandlingTag) bool {
	_, ok := c.handlingTags[tag]
	return ok
}

// IsHazmat is a convenience for the stow-time placement check.
func (c *ProductClassification) IsHazmat() bool { return c.HasTag(Hazmat) }

// IsTemperatureSensitive is a convenience for the stow-time placement check.
func (c *ProductClassification) IsTemperatureSensitive() bool {
	return c.HasTag(TemperatureSensitive)
}

// SlotAttributes is the placement-relevant subset of a facility-layout
// location's attributes, as seen from this bounded context — the result of
// the cross-context lookup StowStock uses to enforce hazmat/temperature
// placement rules. Known=false means "no constraint info available for
// this bin", which StowStock treats as permission to stow: this service
// does not own location modelling and cannot assume every bin is known to
// facility-layout (fail-open for unclassified/unknown bins — see ADR
// 0009).
type SlotAttributes struct {
	Hazmat           bool
	TemperatureClass TemperatureClass
	Known            bool
}

// ProductClassified is the domain event raised when a SKU's classification
// is registered or replaced.
type ProductClassified struct {
	SKU              shared.SKU
	HandlingTags     []HandlingTag
	TemperatureClass TemperatureClass
	At               time.Time
}

func (e ProductClassified) EventName() string     { return "ProductClassified" }
func (e ProductClassified) OccurredAt() time.Time { return e.At }

// NewProductClassified builds the domain event for a construction/update of
// c, using occurredAt as the event's timestamp (supplied by the Clock port,
// not wall-clock time — consistent with every other event in this
// context).
func NewProductClassified(c *ProductClassification, occurredAt time.Time) ProductClassified {
	return ProductClassified{
		SKU:              c.SKU(),
		HandlingTags:     c.HandlingTags(),
		TemperatureClass: c.TemperatureClass(),
		At:               occurredAt,
	}
}
