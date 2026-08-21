package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// EventPublisher persists domain events to the events table (an outbox).
// A separate relay (out of scope here) would forward rows to a broker.
type EventPublisher struct {
	pool *pgxpool.Pool
}

func NewEventPublisher(pool *pgxpool.Pool) *EventPublisher {
	return &EventPublisher{pool: pool}
}

func (p *EventPublisher) Publish(ctx context.Context, event shared.DomainEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO events (event_name, occurred_at, payload) VALUES ($1, $2, $3)
	`, event.EventName(), event.OccurredAt(), payload)
	return err
}
