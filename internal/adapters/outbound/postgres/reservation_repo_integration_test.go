//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/postgres"
	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

func TestPostgres_ReservationRoundTrip(t *testing.T) {
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

	// A reservation's allocations reference stock_units, which reference
	// bins (foreign keys) — seed both first.
	locations := postgres.NewLocationRepo(pool)
	binID, _ := shared.NewBinId("IT-RES-BIN")
	bin, err := location.NewBin(binID, mustQty(t, 10))
	if err != nil {
		t.Fatalf("unexpected error building bin: %v", err)
	}
	if err := locations.Save(ctx, bin); err != nil {
		t.Fatalf("unexpected error saving bin: %v", err)
	}

	stockRepo := postgres.NewStockRepo(pool)
	sku, _ := shared.NewSKU("IT-RES-SKU")
	unit, err := stock.NewStockUnit("it-res-su-1", sku, binID, mustQty(t, 5))
	if err != nil {
		t.Fatalf("unexpected error building stock unit: %v", err)
	}
	if err := stockRepo.Save(ctx, unit); err != nil {
		t.Fatalf("unexpected error saving stock unit: %v", err)
	}

	repo := postgres.NewReservationRepo(pool)
	id, err := repo.NextID(ctx)
	if err != nil {
		t.Fatalf("unexpected error generating ID: %v", err)
	}

	allocs := []reservation.Allocation{{StockUnitID: unit.ID(), Quantity: mustQty(t, 4)}}
	created := time.Now().UTC().Truncate(time.Second)
	res, err := reservation.New(id, sku, mustQty(t, 4), "order-it-1", allocs, created, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error building reservation: %v", err)
	}

	if err := repo.Save(ctx, res); err != nil {
		t.Fatalf("unexpected error saving reservation: %v", err)
	}

	found, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error finding reservation: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find the saved reservation")
	}
	if found.Status() != reservation.StatusActive || found.Quantity().Int() != 4 || len(found.Allocations()) != 1 {
		t.Fatalf("expected round-tripped fields to match, got status=%v quantity=%d allocations=%d",
			found.Status(), found.Quantity().Int(), len(found.Allocations()))
	}
	if found.Allocations()[0].StockUnitID != unit.ID() || found.Allocations()[0].Quantity.Int() != 4 {
		t.Fatalf("expected allocation to round-trip, got %+v", found.Allocations()[0])
	}

	// Query-specific: an update (revoke) persists via the same Save/upsert path.
	if err := found.Revoke(); err != nil {
		t.Fatalf("unexpected error revoking: %v", err)
	}
	if err := repo.Save(ctx, found); err != nil {
		t.Fatalf("unexpected error saving revoked reservation: %v", err)
	}
	refetched, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error re-finding reservation: %v", err)
	}
	if refetched.Status() != reservation.StatusRevoked {
		t.Fatalf("expected status update to persist, got %v", refetched.Status())
	}
}

func TestPostgres_Reservation_FindByID_UnknownReturnsNil(t *testing.T) {
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

	repo := postgres.NewReservationRepo(pool)
	found, err := repo.FindByID(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Fatalf("expected nil for an unknown reservation ID, got %+v", found)
	}
}
