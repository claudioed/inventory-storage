package events

import (
	"context"
	"sync"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// BufferedPublisher accumulates events in memory. Useful for tests that
// assert on which events a use case published, and as a staging buffer
// in front of a real broker adapter.
type BufferedPublisher struct {
	mu     sync.Mutex
	events []shared.DomainEvent
}

func NewBufferedPublisher() *BufferedPublisher {
	return &BufferedPublisher{}
}

func (p *BufferedPublisher) Publish(_ context.Context, event shared.DomainEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return nil
}

// Events returns a snapshot of every event published so far.
func (p *BufferedPublisher) Events() []shared.DomainEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]shared.DomainEvent, len(p.events))
	copy(out, p.events)
	return out
}
