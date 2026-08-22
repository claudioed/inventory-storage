//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/postgres"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

func TestPostgres_EventPublisher_PersistsEvent(t *testing.T) {
	databaseURL := requireDatabaseURL(t)
	if err := postgres.RunMigrations(databaseURL, migrationsDir(t)); err != nil {
		t.Fatalf("unexpected error running migrations: %v", err)
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("unexpected error opening pool: %v", err)
	}
	defer pool.Close()

	publisher := postgres.NewEventPublisher(pool)
	sku, _ := shared.NewSKU("IT-EVT-SKU")
	qty := mustQty(t, 9)
	occurredAt := time.Now().UTC().Truncate(time.Second)
	event := shared.NewStockReceived(occurredAt, sku, qty)

	if err := publisher.Publish(ctx, event); err != nil {
		t.Fatalf("unexpected error publishing event: %v", err)
	}

	// Query-specific: the events table (an outbox) has no repo Find method,
	// so verify the round-trip by reading the row directly, matching on the
	// distinguishing occurred_at timestamp used for this test's event.
	var count int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM events WHERE event_name = $1 AND occurred_at = $2
	`, event.EventName(), occurredAt).Scan(&count)
	if err != nil {
		t.Fatalf("unexpected error querying events table: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 persisted StockReceived event at %v, got %d", occurredAt, count)
	}
}
