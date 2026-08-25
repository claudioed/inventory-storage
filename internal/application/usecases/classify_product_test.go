package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/product"
)

func TestClassifyProduct_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		sku     string
		tags    []product.HandlingTag
		temp    product.TemperatureClass
		wantErr error
	}{
		{
			name: "hazmat succeeds",
			sku:  "SKU-1",
			tags: []product.HandlingTag{product.Hazmat},
		},
		{
			name: "temperature sensitive with class succeeds",
			sku:  "SKU-2",
			tags: []product.HandlingTag{product.TemperatureSensitive},
			temp: product.Frozen,
		},
		{
			name:    "no tags rejected",
			sku:     "SKU-3",
			tags:    nil,
			wantErr: product.ErrNoHandlingTags,
		},
		{
			name:    "temperature sensitive without class rejected",
			sku:     "SKU-4",
			tags:    []product.HandlingTag{product.TemperatureSensitive},
			wantErr: product.ErrTemperatureClassRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			uc := &usecases.ClassifyProduct{Classifications: e.Classifications, Events: e.Events, Clock: e.Clock}

			c, err := uc.Execute(context.Background(), mustSKU(t, tt.sku), tt.tags, tt.temp)
			if err != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr != nil {
				if c != nil {
					t.Fatalf("expected nil classification on error, got %+v", c)
				}
				return
			}
			if c == nil {
				t.Fatalf("expected a classification, got nil")
			}

			stored, err := e.Classifications.FindBySKU(context.Background(), mustSKU(t, tt.sku))
			if err != nil {
				t.Fatalf("unexpected error finding stored classification: %v", err)
			}
			if stored == nil {
				t.Fatalf("expected classification to be persisted")
			}
		})
	}
}

func TestClassifyProduct_PublishesProductClassified(t *testing.T) {
	e := newEnv()
	uc := &usecases.ClassifyProduct{Classifications: e.Classifications, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), []product.HandlingTag{product.Hazmat}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, evt := range e.Events.Events() {
		if evt.EventName() == "ProductClassified" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ProductClassified to be published")
	}
}

// Re-classifying the same SKU replaces its prior classification rather
// than erroring — a legitimate operational action.
func TestClassifyProduct_Reclassify_Replaces(t *testing.T) {
	e := newEnv()
	uc := &usecases.ClassifyProduct{Classifications: e.Classifications, Events: e.Events, Clock: e.Clock}

	sku := mustSKU(t, "SKU-1")
	if _, err := uc.Execute(context.Background(), sku, []product.HandlingTag{product.Fragile}, ""); err != nil {
		t.Fatalf("unexpected error on first classify: %v", err)
	}
	if _, err := uc.Execute(context.Background(), sku, []product.HandlingTag{product.Hazmat}, ""); err != nil {
		t.Fatalf("unexpected error on reclassify: %v", err)
	}

	stored, err := e.Classifications.FindBySKU(context.Background(), sku)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stored.HasTag(product.Hazmat) || stored.HasTag(product.Fragile) {
		t.Fatalf("expected reclassification to replace tags, got %v", stored.HandlingTags())
	}
}

func TestClassifyProduct_SaveFails_PropagatesError(t *testing.T) {
	e := newEnv()
	repo := &failingProductClassificationRepo{delegate: e.Classifications, failSave: true}
	uc := &usecases.ClassifyProduct{Classifications: repo, Events: e.Events, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), []product.HandlingTag{product.Hazmat}, "")
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}

func TestClassifyProduct_EventPublishFails_PropagatesError(t *testing.T) {
	e := newEnv()
	uc := &usecases.ClassifyProduct{Classifications: e.Classifications, Events: failingEvents{}, Clock: e.Clock}

	_, err := uc.Execute(context.Background(), mustSKU(t, "SKU-1"), []product.HandlingTag{product.Hazmat}, "")
	if err != errFake {
		t.Fatalf("expected errFake, got %v", err)
	}
}
