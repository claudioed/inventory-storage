//go:build integration

package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/postgres"
	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// migrationsDir resolves /migrations relative to this test file, so the
// test works regardless of the working directory `go test` is invoked from.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

func requireDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres integration test")
	}
	return url
}

func TestPostgres_BinRoundTrip(t *testing.T) {
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

	repo := postgres.NewLocationRepo(pool)
	binID, _ := shared.NewBinId("IT-BIN-1")
	capacity, _ := shared.NewQuantity(10)
	bin, err := location.NewBin(binID, capacity)
	if err != nil {
		t.Fatalf("unexpected error building bin: %v", err)
	}
	if err := bin.Occupy(mustQty(t, 4)); err != nil {
		t.Fatalf("unexpected error occupying bin: %v", err)
	}

	if err := repo.Save(ctx, bin); err != nil {
		t.Fatalf("unexpected error saving bin: %v", err)
	}

	found, err := repo.FindByID(ctx, binID)
	if err != nil {
		t.Fatalf("unexpected error finding bin: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find the saved bin")
	}
	if found.Occupied().Int() != 4 {
		t.Fatalf("expected occupied=4, got %d", found.Occupied().Int())
	}
}

func mustQty(t *testing.T, v int) shared.Quantity {
	t.Helper()
	q, err := shared.NewQuantity(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return q
}
