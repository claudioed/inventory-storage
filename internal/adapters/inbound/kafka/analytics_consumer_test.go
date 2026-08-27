package kafka_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	inboundkafka "github.com/claudioed/inventory-storage/internal/adapters/inbound/kafka"
)

// call captures one projection-store method invocation.
type call struct {
	method  string
	eventId string
	sku     string
	binId   string
	qty     int
	at      time.Time
}

// fakeProjection records the calls the consumer makes so a test can assert the
// envelope was routed to the right method with the right fields.
type fakeProjection struct {
	calls []call
}

func (f *fakeProjection) ApplyStockReceived(_ context.Context, eventId, sku string, qty int, at time.Time) error {
	f.calls = append(f.calls, call{"received", eventId, sku, "", qty, at})
	return nil
}
func (f *fakeProjection) ApplyItemStowed(_ context.Context, eventId, sku, binId string, at time.Time) error {
	f.calls = append(f.calls, call{"stowed", eventId, sku, binId, 0, at})
	return nil
}
func (f *fakeProjection) ApplyStockPicked(_ context.Context, eventId, sku string, qty int, at time.Time) error {
	f.calls = append(f.calls, call{"picked", eventId, sku, "", qty, at})
	return nil
}
func (f *fakeProjection) ApplyStockReserved(_ context.Context, eventId, sku string, at time.Time) error {
	f.calls = append(f.calls, call{"reserved", eventId, sku, "", 0, at})
	return nil
}
func (f *fakeProjection) ApplyReservationExpired(_ context.Context, eventId, sku string, at time.Time) error {
	f.calls = append(f.calls, call{"expired", eventId, sku, "", 0, at})
	return nil
}
func (f *fakeProjection) ApplyReservationRevoked(_ context.Context, eventId, sku string, at time.Time) error {
	f.calls = append(f.calls, call{"revoked", eventId, sku, "", 0, at})
	return nil
}
func (f *fakeProjection) ApplyCycleCountCompleted(_ context.Context, eventId, binId string, at time.Time) error {
	f.calls = append(f.calls, call{"cycle", eventId, "", binId, 0, at})
	return nil
}
func (f *fakeProjection) ApplyDiscrepancyDetected(_ context.Context, eventId, binId string, at time.Time) error {
	f.calls = append(f.calls, call{"discrepancy", eventId, "", binId, 0, at})
	return nil
}
func (f *fakeProjection) ApplyItemUnlocated(_ context.Context, eventId, sku, binId string, at time.Time) error {
	f.calls = append(f.calls, call{"unlocated", eventId, sku, binId, 0, at})
	return nil
}

// fakeProcessed is an in-memory report.ProcessedEvents.
type fakeProcessed struct {
	seen map[string]bool
}

func newFakeProcessed() *fakeProcessed { return &fakeProcessed{seen: map[string]bool{}} }

func (p *fakeProcessed) MarkProcessed(_ context.Context, eventId string) (bool, error) {
	if p.seen[eventId] {
		return false, nil
	}
	p.seen[eventId] = true
	return true, nil
}

func envelope(t *testing.T, eventId, eventType string, at time.Time, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	env := map[string]any{
		"event_id":       eventId,
		"event_type":     eventType,
		"occurred_at":    at.Format(time.RFC3339Nano),
		"source":         "inventory-storage",
		"schema_version": 1,
		"data":           json.RawMessage(raw),
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

func TestAnalyticsConsumer_RoutesEachEventType(t *testing.T) {
	at := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		eventType  string
		data       map[string]any
		wantMethod string
		wantSKU    string
		wantBin    string
		wantQty    int
	}{
		{"received", "StockReceived", map[string]any{"sku": "SKU-1", "quantity": 10}, "received", "SKU-1", "", 10},
		{"stowed", "ItemStowed", map[string]any{"sku": "SKU-1", "bin_id": "BIN-A"}, "stowed", "SKU-1", "BIN-A", 0},
		{"picked", "StockPicked", map[string]any{"sku": "SKU-1", "quantity": 4}, "picked", "SKU-1", "", 4},
		{"reserved", "StockReserved", map[string]any{"sku": "SKU-1"}, "reserved", "SKU-1", "", 0},
		{"expired", "ReservationExpired", map[string]any{"sku": "SKU-1"}, "expired", "SKU-1", "", 0},
		{"revoked", "ReservationRevoked", map[string]any{"sku": "SKU-1"}, "revoked", "SKU-1", "", 0},
		{"cycle", "CycleCountCompleted", map[string]any{"bin_id": "BIN-A"}, "cycle", "", "BIN-A", 0},
		{"discrepancy", "DiscrepancyDetected", map[string]any{"bin_id": "BIN-A"}, "discrepancy", "", "BIN-A", 0},
		{"unlocated", "ItemUnlocated", map[string]any{"sku": "SKU-1", "bin_id": "BIN-A"}, "unlocated", "SKU-1", "BIN-A", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj := &fakeProjection{}
			processed := newFakeProcessed()
			c := &inboundkafka.AnalyticsConsumer{Projection: proj, Processed: processed, Logger: slog.Default()}

			raw := envelope(t, "e-"+tt.name, tt.eventType, at, tt.data)
			if err := c.HandleMessage(context.Background(), raw); err != nil {
				t.Fatalf("HandleMessage: %v", err)
			}
			if len(proj.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(proj.calls))
			}
			got := proj.calls[0]
			if got.method != tt.wantMethod {
				t.Errorf("method = %q, want %q", got.method, tt.wantMethod)
			}
			if got.sku != tt.wantSKU || got.binId != tt.wantBin || got.qty != tt.wantQty {
				t.Errorf("fields = %+v, want sku=%q bin=%q qty=%d", got, tt.wantSKU, tt.wantBin, tt.wantQty)
			}
			if !got.at.Equal(at) {
				t.Errorf("at = %v, want %v", got.at, at)
			}
		})
	}
}

func TestAnalyticsConsumer_Idempotent(t *testing.T) {
	at := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	proj := &fakeProjection{}
	processed := newFakeProcessed()
	c := &inboundkafka.AnalyticsConsumer{Projection: proj, Processed: processed, Logger: slog.Default()}

	raw := envelope(t, "dup", "StockReceived", at, map[string]any{"sku": "SKU-1", "quantity": 5})
	for range 2 {
		if err := c.HandleMessage(context.Background(), raw); err != nil {
			t.Fatalf("HandleMessage: %v", err)
		}
	}
	if len(proj.calls) != 1 {
		t.Fatalf("expected 1 apply for duplicate delivery, got %d", len(proj.calls))
	}
}

func TestAnalyticsConsumer_IgnoresUnknownEventType(t *testing.T) {
	proj := &fakeProjection{}
	processed := newFakeProcessed()
	c := &inboundkafka.AnalyticsConsumer{Projection: proj, Processed: processed, Logger: slog.Default()}

	// LocationRecorded is on the topic but does not move the report.
	raw := envelope(t, "e1", "LocationRecorded", time.Now(), map[string]any{"bin_id": "BIN-A"})
	if err := c.HandleMessage(context.Background(), raw); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(proj.calls) != 0 {
		t.Fatalf("expected non-projecting event to make no call, got %d", len(proj.calls))
	}
	// An event with no projection method must NOT be marked processed, so a
	// later contract change could reprocess it.
	if processed.seen["e1"] {
		t.Error("non-projecting event should not be marked processed")
	}
}
