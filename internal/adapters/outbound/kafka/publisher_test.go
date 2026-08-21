package kafka_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/kafka"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// fakeWriter captures every message it's asked to write, so tests can assert
// on the exact envelope shape without a real broker.
type fakeWriter struct {
	messages []kafkago.Message
	err      error
}

func (w *fakeWriter) WriteMessages(_ context.Context, msgs ...kafkago.Message) error {
	if w.err != nil {
		return w.err
	}
	w.messages = append(w.messages, msgs...)
	return nil
}

type envelope struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Source     string          `json:"source"`
	Data       json.RawMessage `json:"data"`
}

type reservationData struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	DemandRef string `json:"demand_ref"`
}

func TestPublisher_StockReserved_EnvelopeShape(t *testing.T) {
	writer := &fakeWriter{}
	pub := kafka.NewPublisher(writer, memory.NewReservationRepo())

	sku, _ := shared.NewSKU("SKU-1")
	qty, _ := shared.NewPositiveQuantity(5)
	occurredAt := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)
	event := shared.NewStockReserved(occurredAt, "res-1", sku, qty, "order-42")

	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	if len(writer.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(writer.messages))
	}

	var env envelope
	if err := json.Unmarshal(writer.messages[0].Value, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if env.EventType != "StockReserved" {
		t.Errorf("EventType = %q, want StockReserved", env.EventType)
	}
	if env.Source != kafka.Source {
		t.Errorf("Source = %q, want %q", env.Source, kafka.Source)
	}
	if !env.OccurredAt.Equal(occurredAt) {
		t.Errorf("OccurredAt = %v, want %v", env.OccurredAt, occurredAt)
	}
	if env.EventID == "" {
		t.Error("EventID must not be empty")
	}

	var data reservationData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	if data.SKU != "SKU-1" || data.Quantity != 5 || data.DemandRef != "order-42" {
		t.Errorf("data = %+v, want {SKU-1 5 order-42}", data)
	}
}

func TestPublisher_ReservationRevoked_EnvelopeShape(t *testing.T) {
	writer := &fakeWriter{}
	repo := memory.NewReservationRepo()

	sku, _ := shared.NewSKU("SKU-2")
	qty, _ := shared.NewPositiveQuantity(3)
	res, err := reservation.New("res-2", sku, qty, "order-99",
		[]reservation.Allocation{{StockUnitID: "unit-1", Quantity: qty}},
		time.Date(2026, 8, 21, 21, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatalf("failed to build reservation: %v", err)
	}
	if err := res.Revoke(); err != nil {
		t.Fatalf("failed to revoke reservation: %v", err)
	}
	if err := repo.Save(context.Background(), res); err != nil {
		t.Fatalf("failed to save reservation: %v", err)
	}

	pub := kafka.NewPublisher(writer, repo)

	occurredAt := time.Date(2026, 8, 21, 22, 5, 0, 0, time.UTC)
	event := shared.NewReservationRevoked(occurredAt, "res-2")

	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	if len(writer.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(writer.messages))
	}

	var env envelope
	if err := json.Unmarshal(writer.messages[0].Value, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}
	if env.EventType != "ReservationRevoked" {
		t.Errorf("EventType = %q, want ReservationRevoked", env.EventType)
	}

	var data reservationData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	if data.SKU != "SKU-2" || data.Quantity != 3 || data.DemandRef != "order-99" {
		t.Errorf("data = %+v, want {SKU-2 3 order-99}", data)
	}
}

func TestPublisher_ReservationRevoked_UnknownReservation(t *testing.T) {
	writer := &fakeWriter{}
	pub := kafka.NewPublisher(writer, memory.NewReservationRepo())

	event := shared.NewReservationRevoked(time.Now(), "does-not-exist")
	if err := pub.Publish(context.Background(), event); err != kafka.ErrReservationNotFound {
		t.Errorf("Publish error = %v, want ErrReservationNotFound", err)
	}
	if len(writer.messages) != 0 {
		t.Errorf("expected no message written, got %d", len(writer.messages))
	}
}

func TestPublisher_IgnoresOtherDomainEvents(t *testing.T) {
	writer := &fakeWriter{}
	pub := kafka.NewPublisher(writer, memory.NewReservationRepo())

	sku, _ := shared.NewSKU("SKU-3")
	qty, _ := shared.NewPositiveQuantity(1)
	event := shared.NewStockReceived(time.Now(), sku, qty)

	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if len(writer.messages) != 0 {
		t.Errorf("expected no message written for a non-integration event, got %d", len(writer.messages))
	}
}
