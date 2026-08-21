// Command inventory is the composition root: it wires env config into
// adapters, adapters into use cases, and use cases into the HTTP router.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	inboundhttp "github.com/claudioed/inventory-storage/internal/adapters/inbound/http"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/postgres"
	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	httpAddr := getenv("HTTP_ADDR", ":8080")
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsPath := getenv("MIGRATIONS_PATH", "migrations")

	logger := log.New(os.Stdout, "inventory-storage ", log.LstdFlags)

	stockRepo, locationRepo, reservationRepo, publisher, closePool, err := buildAdapters(databaseURL, migrationsPath, logger)
	if err != nil {
		return err
	}
	defer closePool()

	clock := memory.SystemClock{}

	server := &inboundhttp.Server{
		ReceiveStock:      &usecases.ReceiveStock{Events: publisher, Clock: clock},
		StowStock:         &usecases.StowStock{Stock: stockRepo, Locations: locationRepo, Events: publisher, Clock: clock},
		ReserveStock:      &usecases.ReserveStock{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		RevokeReservation: &usecases.RevokeReservation{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		ConfirmPick:       &usecases.ConfirmPick{Stock: stockRepo, Locations: locationRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		GetUsable:         &usecases.GetUsable{Stock: stockRepo},
		RunCycleCount:     &usecases.RunCycleCount{Stock: stockRepo, Events: publisher, Clock: clock},
	}

	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           inboundhttp.NewRouter(server),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// buildAdapters wires the Postgres adapters when DATABASE_URL is set, or
// falls back to the in-memory adapters for local development without a
// database.
func buildAdapters(databaseURL, migrationsPath string, logger *log.Logger) (
	ports.StockRepo, ports.LocationRepo, ports.ReservationRepo, ports.EventPublisher, func(), error,
) {
	noop := func() {}

	if databaseURL == "" {
		logger.Println("DATABASE_URL not set; using in-memory adapters")
		return memory.NewStockRepo(), memory.NewLocationRepo(), memory.NewReservationRepo(), events.NewLogPublisher(logger), noop, nil
	}

	if err := postgres.RunMigrations(databaseURL, migrationsPath); err != nil {
		return nil, nil, nil, nil, noop, err
	}

	pool, err := postgres.NewPool(context.Background(), databaseURL)
	if err != nil {
		return nil, nil, nil, nil, noop, err
	}

	return postgres.NewStockRepo(pool),
		postgres.NewLocationRepo(pool),
		postgres.NewReservationRepo(pool),
		postgres.NewEventPublisher(pool),
		pool.Close,
		nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
