package shared

import (
	"testing"
	"time"
)

func TestEvents_NameAndOccurredAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sku, _ := NewSKU("SKU-1")
	binID, _ := NewBinId("A-1-1")
	qty, _ := NewQuantity(5)

	tests := []struct {
		name  string
		event DomainEvent
		want  string
	}{
		{"StockReceived", NewStockReceived(now, sku, qty), "StockReceived"},
		{"ItemStowed", NewItemStowed(now, sku, binID, qty), "ItemStowed"},
		{"LocationRecorded", NewLocationRecorded(now, "su-1", binID), "LocationRecorded"},
		{"StockReserved", NewStockReserved(now, "res-1", sku, qty, "order-1"), "StockReserved"},
		{"ReservationExpired", NewReservationExpired(now, "res-1"), "ReservationExpired"},
		{"ReservationRevoked", NewReservationRevoked(now, "res-1"), "ReservationRevoked"},
		{"StockPicked", NewStockPicked(now, "res-1", sku, qty), "StockPicked"},
		{"ItemUnlocated", NewItemUnlocated(now, "su-1", sku, binID, qty), "ItemUnlocated"},
		{"CycleCountCompleted", NewCycleCountCompleted(now, binID, qty, qty, false), "CycleCountCompleted"},
		{"DiscrepancyDetected", NewDiscrepancyDetected(now, binID, qty, qty), "DiscrepancyDetected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.EventName(); got != tt.want {
				t.Fatalf("expected EventName()=%q, got %q", tt.want, got)
			}
			if !tt.event.OccurredAt().Equal(now) {
				t.Fatalf("expected OccurredAt()=%v, got %v", now, tt.event.OccurredAt())
			}
		})
	}
}

func TestItemStowed_CarriesFields(t *testing.T) {
	now := time.Now()
	sku, _ := NewSKU("SKU-1")
	binID, _ := NewBinId("A-1-1")
	qty, _ := NewQuantity(5)

	ev := NewItemStowed(now, sku, binID, qty)
	if ev.SKU != sku || ev.BinID != binID || ev.Quantity != qty {
		t.Fatalf("expected fields to round-trip, got %+v", ev)
	}
}

func TestDiscrepancyDetected_CarriesFields(t *testing.T) {
	now := time.Now()
	binID, _ := NewBinId("A-1-1")
	counted, _ := NewQuantity(3)
	system, _ := NewQuantity(5)

	ev := NewDiscrepancyDetected(now, binID, counted, system)
	if ev.BinID != binID || ev.CountedQty != counted || ev.SystemQty != system {
		t.Fatalf("expected fields to round-trip, got %+v", ev)
	}
}
