package events

import (
	"context"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// MultiPublisher fans one domain event out to several EventPublishers in
// order, so the same event can be forwarded to more than one destination
// (e.g. the Kafka integration topic AND the analytics topic) behind the single
// ports.EventPublisher the use cases depend on. Publish stops and returns the
// first error, so a delivery failure to any destination surfaces to the use
// case rather than being silently swallowed.
type MultiPublisher struct {
	publishers []ports.EventPublisher
}

// NewMultiPublisher builds a MultiPublisher over publishers, applied in the
// given order. A nil publisher in the list is skipped.
func NewMultiPublisher(publishers ...ports.EventPublisher) *MultiPublisher {
	return &MultiPublisher{publishers: publishers}
}

// Publish forwards event to every configured publisher in order, returning the
// first error encountered.
func (m *MultiPublisher) Publish(ctx context.Context, event shared.DomainEvent) error {
	for _, p := range m.publishers {
		if p == nil {
			continue
		}
		if err := p.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// Compile-time assertion that MultiPublisher satisfies the port.
var _ ports.EventPublisher = (*MultiPublisher)(nil)
