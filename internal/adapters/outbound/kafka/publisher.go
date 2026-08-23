// Package kafka publishes cross-service integration events to the shared
// warehouse-systems Kafka broker. It implements ports.EventPublisher, so it
// drops in wherever the log or Postgres outbox publisher is used today.
//
// Only StockReserved and ReservationRevoked are part of the published
// integration contract (see CLAUDE.md's Cross-service integration section);
// every other domain event is a local concern and is not forwarded here.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// Topic is the integration events topic this service publishes to.
const Topic = "warehouse.inventory.events"

// Source identifies this service in the event envelope.
const Source = "inventory-storage"

// tracerName scopes the publish spans this adapter emits.
const tracerName = "github.com/claudioed/inventory-storage/internal/adapters/outbound/kafka"

// spanName follows the fleet-wide convention for messaging spans:
// "kafka.publish <topic>" on the producer side, "kafka.consume <topic>" on
// the consumer side. This service only publishes.
const spanName = "kafka.publish " + Topic

// ErrReservationNotFound is returned when a ReservationRevoked event
// references a reservation the repo no longer has (should not happen: the
// use case saves the reservation before publishing).
var ErrReservationNotFound = errors.New("kafka publisher: reservation not found for revoked event")

// Writer is the subset of *kafkago.Writer the Publisher depends on, so unit
// tests can substitute a fake without a real broker.
type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafkago.Message) error
}

// envelope is the integration event wrapper shared across all
// warehouse-systems services.
type envelope struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Source     string          `json:"source"`
	Data       json.RawMessage `json:"data"`
}

// reservationData is the `data` payload shape for both StockReserved and
// ReservationRevoked, per CLAUDE.md.
type reservationData struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	DemandRef string `json:"demand_ref"`
}

// Publisher publishes StockReserved and ReservationRevoked domain events as
// integration events on Topic.
type Publisher struct {
	writer       Writer
	reservations ports.ReservationRepo
}

// NewPublisher builds a Publisher. reservations is used to look up the SKU,
// quantity, and demand reference for a ReservationRevoked event, since the
// domain event itself carries only the reservation id.
func NewPublisher(writer Writer, reservations ports.ReservationRepo) *Publisher {
	return &Publisher{writer: writer, reservations: reservations}
}

// NewWriter builds a *kafkago.Writer addressed at Topic on the given broker
// addresses.
func NewWriter(brokers ...string) *kafkago.Writer {
	return &kafkago.Writer{
		Addr:                   kafkago.TCP(brokers...),
		Topic:                  Topic,
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
}

func (p *Publisher) Publish(ctx context.Context, event shared.DomainEvent) error {
	var data reservationData

	switch e := event.(type) {
	case shared.StockReserved:
		data = reservationData{SKU: e.SKU.String(), Quantity: e.Quantity.Int(), DemandRef: e.DemandRef}
	case shared.ReservationRevoked:
		res, err := p.reservations.FindByID(ctx, e.ReservationID)
		if err != nil {
			return err
		}
		if res == nil {
			return ErrReservationNotFound
		}
		data = reservationData{SKU: res.SKU().String(), Quantity: res.Quantity().Int(), DemandRef: res.DemandRef()}
	default:
		return nil
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	env := envelope{
		EventID:    uuid.NewString(),
		EventType:  event.EventName(),
		OccurredAt: event.OccurredAt(),
		Source:     Source,
		Data:       payload,
	}

	msg, err := json.Marshal(env)
	if err != nil {
		return err
	}

	ctx, span := otel.Tracer(tracerName).Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingDestinationName(Topic),
			semconv.MessagingOperationName("publish"),
			semconv.MessagingMessageID(env.EventID),
			attribute.String("messaging.message.event_type", env.EventType),
		),
	)
	defer span.End()

	// Inject after starting the span so the headers carry *this* span as the
	// parent: that is what stitches the downstream consumer's trace onto
	// this one.
	headers := []kafkago.Header{}
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier{headers: &headers})

	if err := p.writer.WriteMessages(ctx, kafkago.Message{Value: msg, Headers: headers}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	return nil
}
