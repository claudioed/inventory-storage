// Package postgres provides pgxpool-backed implementations of every
// outbound port, plus a golang-migrate runner for the SQL migrations in
// /migrations.
package postgres

import (
	"context"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool opens a connection pool against databaseURL, traced by otelpgx so
// every query, batch, copy and pool acquire becomes a child span of whatever
// span is active on the calling context.
//
// otelpgx records the *normalized* statement, never the bound arguments, so
// SKUs, bin codes and demand references never leave the process as span
// attributes.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	// pgxpool promotes this to its acquire tracer too, since otelpgx's
	// Tracer also satisfies pgxpool.AcquireTracer.
	config.ConnConfig.Tracer = otelpgx.NewTracer()

	return pgxpool.NewWithConfig(ctx, config)
}
