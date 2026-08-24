package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/product"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

func mustQty(t *testing.T, v int) shared.Quantity {
	t.Helper()
	q, err := shared.NewQuantity(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return q
}

func mustSKU(t *testing.T, v string) shared.SKU {
	t.Helper()
	sku, err := shared.NewSKU(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return sku
}

func mustBinID(t *testing.T, v string) shared.BinId {
	t.Helper()
	binID, err := shared.NewBinId(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return binID
}

func seedBin(t *testing.T, e env, binID shared.BinId, capacity int) {
	t.Helper()
	bin, err := location.NewBin(binID, mustQty(t, capacity))
	if err != nil {
		t.Fatalf("unexpected error seeding bin: %v", err)
	}
	if err := e.Locations.Save(context.Background(), bin); err != nil {
		t.Fatalf("unexpected error saving bin: %v", err)
	}
}

func TestStowStock_ValidItemAndLocation_Succeeds(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	unit, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Quantity().Int() != 5 {
		t.Fatalf("expected quantity=5, got %d", unit.Quantity().Int())
	}

	bin, _ := e.Locations.FindByID(context.Background(), mustBinID(t, "A-1-1"))
	if bin.Occupied().Int() != 5 {
		t.Fatalf("expected bin occupied=5, got %d", bin.Occupied().Int())
	}
}

// Named invariant: "bin-capacity rejection" — enforced end-to-end through the use case.
func TestStowStock_ExceedsBinCapacity_Rejected(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 5)
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 6), mustBinID(t, "A-1-1"))
	if err != location.ErrBinFull {
		t.Fatalf("expected ErrBinFull, got %v", err)
	}
}

func TestStowStock_UnknownBin_Rejected(t *testing.T) {
	e := newEnv()
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 1), mustBinID(t, "A-1-1"))
	if err != usecases.ErrBinNotFound {
		t.Fatalf("expected ErrBinNotFound, got %v", err)
	}
}

func TestStowStock_LocationsFindByIDFails_PropagatesError(t *testing.T) {
	e := newEnv()
	locs := &failingLocationRepo{delegate: e.Locations, failFindByID: true}
	uc := &usecases.StowStock{Stock: e.Stock, Locations: locs, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 1), mustBinID(t, "A-1-1"))
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestStowStock_StockNextIDFails_PropagatesError(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	stockRepo := &failingStockRepo{delegate: e.Stock, failNextID: true}
	uc := &usecases.StowStock{Stock: stockRepo, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 1), mustBinID(t, "A-1-1"))
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestStowStock_LocationsSaveFails_PropagatesError(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	locs := &failingLocationRepo{delegate: e.Locations, failSave: true}
	uc := &usecases.StowStock{Stock: e.Stock, Locations: locs, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 1), mustBinID(t, "A-1-1"))
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestStowStock_StockSaveFails_PropagatesError(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	stockRepo := &failingStockRepo{delegate: e.Stock, failSave: true}
	uc := &usecases.StowStock{Stock: stockRepo, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 1), mustBinID(t, "A-1-1"))
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestStowStock_EventPublishFails_PropagatesError(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: failingEvents{}, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 1), mustBinID(t, "A-1-1"))
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

// --------------------------------------------------------------------
// Placement checks (ADR 0009): hazmat/temperature rules enforced against
// facility-layout's location-classification lookup.
// --------------------------------------------------------------------

// classifyAndSave registers a ProductClassification directly through the
// repo, bypassing ClassifyProduct's own event publish, so these tests stay
// focused on StowStock's behaviour.
func classifyAndSave(t *testing.T, e env, sku shared.SKU, tags []product.HandlingTag, temp product.TemperatureClass) {
	t.Helper()
	c, err := product.New(sku, tags, temp)
	if err != nil {
		t.Fatalf("unexpected error building classification: %v", err)
	}
	if err := e.Classifications.Save(context.Background(), c); err != nil {
		t.Fatalf("unexpected error saving classification: %v", err)
	}
}

// Nil-safe: a StowStock built without Classifications/LocationLookup (as
// every test above this section does) behaves exactly as before this
// feature existed — no placement check at all, even for a SKU that
// happens to share a name with a classified one in another test's env.
func TestStowStock_NoClassificationPorts_PermissiveByDefault(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	uc := &usecases.StowStock{Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Unclassified SKU: even with both ports wired, no lookup-blocking occurs
// because there is nothing to enforce.
func TestStowStock_UnclassifiedSKU_NotBlocked(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	lookup := &fakeLocationLookup{failErr: errFake} // would fail-closed if consulted
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("expected unclassified sku to never be blocked, got %v", err)
	}
}

func TestStowStock_Hazmat_HazmatRatedZone_Succeeds(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	classifyAndSave(t, e, mustSKU(t, "SKU-1"), []product.HandlingTag{product.Hazmat}, "")
	lookup := &fakeLocationLookup{attrs: map[shared.BinId]product.SlotAttributes{
		"A-1-1": {Hazmat: true, Known: true},
	}}
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStowStock_Hazmat_NonHazmatZone_Rejected(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	classifyAndSave(t, e, mustSKU(t, "SKU-1"), []product.HandlingTag{product.Hazmat}, "")
	lookup := &fakeLocationLookup{attrs: map[shared.BinId]product.SlotAttributes{
		"A-1-1": {Hazmat: false, Known: true},
	}}
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != usecases.ErrHazmatZoneRequired {
		t.Fatalf("expected ErrHazmatZoneRequired, got %v", err)
	}
}

func TestStowStock_TemperatureSensitive_MatchingClass_Succeeds(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	classifyAndSave(t, e, mustSKU(t, "SKU-1"), []product.HandlingTag{product.TemperatureSensitive}, product.Chilled)
	lookup := &fakeLocationLookup{attrs: map[shared.BinId]product.SlotAttributes{
		"A-1-1": {TemperatureClass: product.Chilled, Known: true},
	}}
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStowStock_TemperatureSensitive_MismatchedClass_Rejected(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	classifyAndSave(t, e, mustSKU(t, "SKU-1"), []product.HandlingTag{product.TemperatureSensitive}, product.Frozen)
	lookup := &fakeLocationLookup{attrs: map[shared.BinId]product.SlotAttributes{
		"A-1-1": {TemperatureClass: product.Ambient, Known: true},
	}}
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != usecases.ErrTemperatureClassMismatch {
		t.Fatalf("expected ErrTemperatureClassMismatch, got %v", err)
	}
}

// Unknown bin (Known=false): fail-open — permits the stow even for a
// classified, rule-relevant SKU.
func TestStowStock_ClassifiedSKU_UnknownBin_FailsOpen(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	classifyAndSave(t, e, mustSKU(t, "SKU-1"), []product.HandlingTag{product.Hazmat}, "")
	lookup := &fakeLocationLookup{} // no entry for A-1-1 -> Known=false
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("expected fail-open for unknown bin, got %v", err)
	}
}

// Transport/5xx failure: fail-closed, but ONLY for a classified SKU that
// actually carries Hazmat or TemperatureSensitive.
func TestStowStock_ClassifiedSKU_TransportError_FailsClosed(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	classifyAndSave(t, e, mustSKU(t, "SKU-1"), []product.HandlingTag{product.Hazmat}, "")
	lookup := &fakeLocationLookup{failErr: errFake}
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != usecases.ErrLocationClassificationUnavailable {
		t.Fatalf("expected ErrLocationClassificationUnavailable, got %v", err)
	}
}

// A classified SKU with no rule-relevant tag (e.g. Fragile alone) is never
// blocked by lookup unavailability — the lookup is not even consulted.
func TestStowStock_NonRuleRelevantClassification_TransportError_NotBlocked(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	classifyAndSave(t, e, mustSKU(t, "SKU-1"), []product.HandlingTag{product.Fragile}, "")
	lookup := &fakeLocationLookup{failErr: errFake}
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: e.Classifications, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != nil {
		t.Fatalf("expected fragile-only classification to never be blocked by lookup failure, got %v", err)
	}
}

func TestStowStock_ClassificationsFindBySKUFails_PropagatesError(t *testing.T) {
	e := newEnv()
	seedBin(t, e, mustBinID(t, "A-1-1"), 10)
	repo := &failingProductClassificationRepo{delegate: e.Classifications, failFindBySKU: true}
	lookup := &fakeLocationLookup{}
	uc := &usecases.StowStock{
		Stock: e.Stock, Locations: e.Locations, Events: e.Events, Clock: e.Clock,
		Classifications: repo, LocationLookup: lookup,
	}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), mustQty(t, 5), mustBinID(t, "A-1-1"))
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}
