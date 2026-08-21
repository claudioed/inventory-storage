// Package events provides outbound EventPublisher implementations. The
// interface is intentionally the shape a Kafka producer would satisfy
// (Publish(ctx, event) error), so a kafka.Publisher can be dropped in later
// without touching the application layer.
package events

import (
	"context"
	"encoding/json"
	"log"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// LogPublisher publishes domain events by logging them as JSON. Useful for
// local development and as a default when no broker is configured.
type LogPublisher struct {
	logger *log.Logger
}

func NewLogPublisher(logger *log.Logger) *LogPublisher {
	return &LogPublisher{logger: logger}
}

func (p *LogPublisher) Publish(_ context.Context, event shared.DomainEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	p.logger.Printf("event=%s payload=%s", event.EventName(), payload)
	return nil
}
