package analyticsstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/inventory-storage/internal/analytics/report"
)

// PostgresProjection is the WRITER implementation of report.ProjectionStore,
// backed by a pgxpool over the analytical database. Every Apply* runs in a
// transaction that first claims the event id in analytics_processed_events
// (ON CONFLICT DO NOTHING); it only mutates the rollup when the claim is new,
// making each apply idempotent per eventId under Kafka's at-least-once
// delivery. It is the only writer of the analytical database.
type PostgresProjection struct {
	pool *pgxpool.Pool
}

// NewPostgresProjection constructs a PostgresProjection over pool.
func NewPostgresProjection(pool *pgxpool.Pool) *PostgresProjection {
	return &PostgresProjection{pool: pool}
}

// claim inserts eventId into analytics_processed_events, returning true iff
// this call newly recorded it (so the caller should apply the effect). It runs
// inside tx so the claim and the effect commit atomically.
func claim(ctx context.Context, tx pgx.Tx, eventId string, occurredAt time.Time) (bool, error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO analytics_processed_events (event_id, occurred_at)
		 VALUES ($1, $2) ON CONFLICT (event_id) DO NOTHING`,
		eventId, occurredAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// inTx runs fn in a transaction, committing on success and rolling back on
// error.
func (p *PostgresProjection) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// rollupDelta is the set of counter/quantity increments a single event
// contributes to a flow-and-accuracy row.
type rollupDelta struct {
	receivedQuantity      int
	stowedCount           int
	pickedQuantity        int
	reservationsCreated   int
	reservationsExpired   int
	reservationsRevoked   int
	cycleCountsCompleted  int
	discrepanciesDetected int
	unlocatedCount        int
}

// applyDelta claims eventId and, when new, adds delta to the (sku, binId, hour)
// row. It is the single idempotent write path every Apply* method funnels
// through.
func (p *PostgresProjection) applyDelta(ctx context.Context, eventId, sku, binId string, at time.Time, delta rollupDelta) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		isNew, err := claim(ctx, tx, eventId, at)
		if err != nil {
			return fmt.Errorf("analyticsstore: claim event: %w", err)
		}
		if !isNew {
			return nil
		}
		return upsertRollup(ctx, tx, sku, binId, at, delta)
	})
}

// ApplyStockReceived adds qty to the (sku, hour) row's received quantity.
func (p *PostgresProjection) ApplyStockReceived(ctx context.Context, eventId, sku string, qty int, at time.Time) error {
	return p.applyDelta(ctx, eventId, sku, "", at, rollupDelta{receivedQuantity: qty})
}

// ApplyItemStowed increments the (sku, binId, hour) row's stowed count.
func (p *PostgresProjection) ApplyItemStowed(ctx context.Context, eventId, sku, binId string, at time.Time) error {
	return p.applyDelta(ctx, eventId, sku, binId, at, rollupDelta{stowedCount: 1})
}

// ApplyStockPicked adds qty to the (sku, hour) row's picked quantity.
func (p *PostgresProjection) ApplyStockPicked(ctx context.Context, eventId, sku string, qty int, at time.Time) error {
	return p.applyDelta(ctx, eventId, sku, "", at, rollupDelta{pickedQuantity: qty})
}

// ApplyStockReserved increments the (sku, hour) row's created-reservation count.
func (p *PostgresProjection) ApplyStockReserved(ctx context.Context, eventId, sku string, at time.Time) error {
	return p.applyDelta(ctx, eventId, sku, "", at, rollupDelta{reservationsCreated: 1})
}

// ApplyReservationExpired increments the (sku, hour) row's expired count.
func (p *PostgresProjection) ApplyReservationExpired(ctx context.Context, eventId, sku string, at time.Time) error {
	return p.applyDelta(ctx, eventId, sku, "", at, rollupDelta{reservationsExpired: 1})
}

// ApplyReservationRevoked increments the (sku, hour) row's revoked count.
func (p *PostgresProjection) ApplyReservationRevoked(ctx context.Context, eventId, sku string, at time.Time) error {
	return p.applyDelta(ctx, eventId, sku, "", at, rollupDelta{reservationsRevoked: 1})
}

// ApplyCycleCountCompleted increments the (binId, hour) row's cycle-count count.
func (p *PostgresProjection) ApplyCycleCountCompleted(ctx context.Context, eventId, binId string, at time.Time) error {
	return p.applyDelta(ctx, eventId, "", binId, at, rollupDelta{cycleCountsCompleted: 1})
}

// ApplyDiscrepancyDetected increments the (binId, hour) row's discrepancy count.
func (p *PostgresProjection) ApplyDiscrepancyDetected(ctx context.Context, eventId, binId string, at time.Time) error {
	return p.applyDelta(ctx, eventId, "", binId, at, rollupDelta{discrepanciesDetected: 1})
}

// ApplyItemUnlocated increments the (sku, binId, hour) row's unlocated count.
func (p *PostgresProjection) ApplyItemUnlocated(ctx context.Context, eventId, sku, binId string, at time.Time) error {
	return p.applyDelta(ctx, eventId, sku, binId, at, rollupDelta{unlocatedCount: 1})
}

// upsertRollup adds delta into the (sku, bin_id, hour_bucket) row, inserting it
// if absent. hour_bucket is derived by truncating at to the UTC hour.
func upsertRollup(ctx context.Context, tx pgx.Tx, sku, binId string, at time.Time, delta rollupDelta) error {
	bucket := at.UTC().Truncate(time.Hour)
	_, err := tx.Exec(ctx,
		`INSERT INTO flow_accuracy_rollup (
			sku, bin_id, hour_bucket,
			received_quantity, stowed_count, picked_quantity,
			reservations_created, reservations_expired, reservations_revoked,
			cycle_counts_completed, discrepancies_detected, unlocated_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (sku, bin_id, hour_bucket) DO UPDATE SET
			received_quantity      = flow_accuracy_rollup.received_quantity + EXCLUDED.received_quantity,
			stowed_count           = flow_accuracy_rollup.stowed_count + EXCLUDED.stowed_count,
			picked_quantity        = flow_accuracy_rollup.picked_quantity + EXCLUDED.picked_quantity,
			reservations_created   = flow_accuracy_rollup.reservations_created + EXCLUDED.reservations_created,
			reservations_expired   = flow_accuracy_rollup.reservations_expired + EXCLUDED.reservations_expired,
			reservations_revoked   = flow_accuracy_rollup.reservations_revoked + EXCLUDED.reservations_revoked,
			cycle_counts_completed = flow_accuracy_rollup.cycle_counts_completed + EXCLUDED.cycle_counts_completed,
			discrepancies_detected = flow_accuracy_rollup.discrepancies_detected + EXCLUDED.discrepancies_detected,
			unlocated_count        = flow_accuracy_rollup.unlocated_count + EXCLUDED.unlocated_count`,
		sku, binId, bucket,
		delta.receivedQuantity, delta.stowedCount, delta.pickedQuantity,
		delta.reservationsCreated, delta.reservationsExpired, delta.reservationsRevoked,
		delta.cycleCountsCompleted, delta.discrepanciesDetected, delta.unlocatedCount)
	if err != nil {
		return fmt.Errorf("analyticsstore: upsert rollup: %w", err)
	}
	return nil
}

// Compile-time assertion that PostgresProjection satisfies the write port.
var _ report.ProjectionStore = (*PostgresProjection)(nil)
