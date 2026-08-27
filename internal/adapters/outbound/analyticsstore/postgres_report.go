package analyticsstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/inventory-storage/internal/analytics/report"
)

// PostgresReport is the READER implementation of report.ReportStore, backed by
// a pgxpool over the analytical database. The pool it is given is expected to
// be pinned to a read-only role / default_transaction_read_only=on, so a bug
// in the reader cannot mutate the read model (ADR-0011). The reader never
// issues writes.
type PostgresReport struct {
	pool *pgxpool.Pool
}

// NewPostgresReport constructs a PostgresReport over pool.
func NewPostgresReport(pool *pgxpool.Pool) *PostgresReport {
	return &PostgresReport{pool: pool}
}

// Query returns the flow-and-accuracy rows matching q. From is inclusive, To
// is exclusive; empty SKU/BinId disables that filter.
func (r *PostgresReport) Query(ctx context.Context, q report.ReportQuery) (report.FlowAccuracyReport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT sku, bin_id, hour_bucket,
			received_quantity, stowed_count, picked_quantity,
			reservations_created, reservations_expired, reservations_revoked,
			cycle_counts_completed, discrepancies_detected, unlocated_count
		 FROM flow_accuracy_rollup
		 WHERE hour_bucket >= $1 AND hour_bucket < $2
		   AND ($3 = '' OR sku = $3)
		   AND ($4 = '' OR bin_id = $4)
		 ORDER BY hour_bucket, sku, bin_id`,
		q.From, q.To, q.SKU, q.BinId)
	if err != nil {
		return report.FlowAccuracyReport{}, fmt.Errorf("analyticsstore: query rollup: %w", err)
	}
	defer rows.Close()

	var out report.FlowAccuracyReport
	for rows.Next() {
		var (
			row    report.Row
			bucket time.Time
		)
		if err := rows.Scan(
			&row.Key.SKU, &row.Key.BinId, &bucket,
			&row.ReceivedQuantity, &row.StowedCount, &row.PickedQuantity,
			&row.ReservationsCreated, &row.ReservationsExpired, &row.ReservationsRevoked,
			&row.CycleCountsCompleted, &row.DiscrepanciesDetected, &row.UnlocatedCount,
		); err != nil {
			return report.FlowAccuracyReport{}, fmt.Errorf("analyticsstore: scan row: %w", err)
		}
		row.Key.HourBucket = bucket.UTC()
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return report.FlowAccuracyReport{}, fmt.Errorf("analyticsstore: iterate rows: %w", err)
	}
	return out, nil
}

// FreshnessLag returns now minus the most recent event's occurred_at, i.e. how
// far the read model trails real time. Zero when the read model is empty or
// (defensively) when the latest event is future-dated.
func (r *PostgresReport) FreshnessLag(ctx context.Context) (time.Duration, error) {
	// max() over an empty table returns a single NULL row (not zero rows), so
	// scan into a nullable *time.Time and treat NULL as "read model empty".
	var latest *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT max(occurred_at) FROM analytics_processed_events`).Scan(&latest)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("analyticsstore: freshness query: %w", err)
	}
	if latest == nil || latest.IsZero() {
		return 0, nil
	}
	lag := time.Since(*latest)
	if lag < 0 {
		return 0, nil
	}
	return lag, nil
}

// Compile-time assertion that PostgresReport satisfies the read port.
var _ report.ReportStore = (*PostgresReport)(nil)
