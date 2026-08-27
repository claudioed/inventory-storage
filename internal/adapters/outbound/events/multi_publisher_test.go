package events_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

type recordingPublisher struct {
	count int
	err   error
}

func (p *recordingPublisher) Publish(context.Context, shared.DomainEvent) error {
	p.count++
	return p.err
}

func TestMultiPublisher_FansOutToAll(t *testing.T) {
	a := &recordingPublisher{}
	b := &recordingPublisher{}
	m := events.NewMultiPublisher(a, nil, b)

	sku, _ := shared.NewSKU("SKU-1")
	qty, _ := shared.NewQuantity(1)
	if err := m.Publish(context.Background(), shared.NewStockReceived(time.Now(), sku, qty)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if a.count != 1 || b.count != 1 {
		t.Errorf("fan-out counts = a:%d b:%d, want 1,1", a.count, b.count)
	}
}

func TestMultiPublisher_ReturnsFirstError(t *testing.T) {
	boom := errors.New("boom")
	a := &recordingPublisher{err: boom}
	b := &recordingPublisher{}
	m := events.NewMultiPublisher(a, b)

	sku, _ := shared.NewSKU("SKU-1")
	qty, _ := shared.NewQuantity(1)
	err := m.Publish(context.Background(), shared.NewStockReceived(time.Now(), sku, qty))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if b.count != 0 {
		t.Errorf("expected fan-out to stop on first error; b.count = %d", b.count)
	}
}
