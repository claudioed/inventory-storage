// Command inventory is the composition root: it wires env config into
// adapters, adapters into use cases, and use cases into the HTTP router.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	inboundhttp "github.com/claudioed/inventory-storage/internal/adapters/inbound/http"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	kafkaadapter "github.com/claudioed/inventory-storage/internal/adapters/outbound/kafka"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/postgres"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/telemetry"
	"github.com/claudioed/inventory-storage/internal/application/ports"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

// telemetryFlushTimeout bounds the final export attempt. Without a deadline
// the exporter would retry against an unreachable Collector and stretch a
// shutdown out well past what an orchestrator will wait for.
const telemetryFlushTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	serviceName := getenv("OTEL_SERVICE_NAME", inboundhttp.DefaultServiceName)
	otlpEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", telemetry.DefaultEndpoint)

	// Telemetry comes up before any adapter, so the pgx pool and the Kafka
	// writer are built against the real providers rather than the no-op
	// globals. Export is non-blocking: an unreachable Collector costs
	// telemetry, never availability.
	shutdownTelemetry, err := telemetry.Setup(context.Background(), serviceName, getenv("SERVICE_VERSION", telemetry.DefaultServiceVersion), otlpEndpoint)
	if err != nil {
		return err
	}
	// Registered before every other defer so it runs last: the final flush
	// happens once the HTTP server has stopped and the adapters are closed.
	// A failed flush is logged, never returned — an unreachable Collector
	// costs telemetry, and turning that into a non-zero exit would make
	// every clean shutdown look like a crash.
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
		defer cancel()
		if err := shutdownTelemetry(flushCtx); err != nil {
			logger.Warn("telemetry flush failed on shutdown", "error", err)
		}
	}()
	logger.Info("telemetry configured", "service_name", serviceName, "otlp_endpoint", otlpEndpoint)

	httpAddr := getenv("HTTP_ADDR", ":8080")
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsPath := getenv("MIGRATIONS_PATH", "migrations")
	eventPublisher := getenv("EVENT_PUBLISHER", "log")

	stockRepo, locationRepo, reservationRepo, publisher, closeAdapters, err := buildAdapters(databaseURL, migrationsPath, eventPublisher, logger)
	if err != nil {
		return err
	}
	defer closeAdapters()

	reservationMetrics, err := telemetry.NewReservationMetrics()
	if err != nil {
		return err
	}

	clock := memory.SystemClock{}

	server := &inboundhttp.Server{
		ReceiveStock:      &usecases.ReceiveStock{Events: publisher, Clock: clock},
		StowStock:         &usecases.StowStock{Stock: stockRepo, Locations: locationRepo, Events: publisher, Clock: clock},
		ReserveStock:      &usecases.ReserveStock{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock, Metrics: reservationMetrics},
		RevokeReservation: &usecases.RevokeReservation{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock, Metrics: reservationMetrics},
		ConfirmPick:       &usecases.ConfirmPick{Stock: stockRepo, Locations: locationRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		GetUsable:         &usecases.GetUsable{Stock: stockRepo},
		RunCycleCount:     &usecases.RunCycleCount{Stock: stockRepo, Events: publisher, Clock: clock},
	}

	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           inboundhttp.NewRouter(server, logger, serviceName),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpAddr)
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

// newLogger builds the process-wide structured logger. LOG_LEVEL maps
// debug|info|warn|error (case-insensitive) to the matching slog.Level,
// defaulting to Info for unset or unrecognized values. Logs are emitted as
// JSON to stdout, wrapped so that any record written with a span-carrying
// context also carries trace_id/span_id.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	return slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

// buildAdapters wires the Postgres adapters when DATABASE_URL is set, or
// falls back to the in-memory adapters for local development without a
// database. The event publisher defaults to that same memory/Postgres
// choice ("log"), or can be switched to the Kafka integration-events
// publisher via eventPublisher="kafka" (EVENT_PUBLISHER env), independent of
// which repos are in use.
func buildAdapters(databaseURL, migrationsPath, eventPublisher string, logger *slog.Logger) (
	ports.StockRepo, ports.LocationRepo, ports.ReservationRepo, ports.EventPublisher, func(), error,
) {
	noop := func() {}

	var (
		stockRepo       ports.StockRepo
		locationRepo    ports.LocationRepo
		reservationRepo ports.ReservationRepo
		defaultPub      ports.EventPublisher
		closeRepos      = noop
	)

	if databaseURL == "" {
		logger.Info("database url not configured; using in-memory adapters")
		stockRepo = memory.NewStockRepo()
		locationRepo = memory.NewLocationRepo()
		reservationRepo = memory.NewReservationRepo()
		defaultPub = events.NewLogPublisher(logger)
	} else {
		if err := postgres.RunMigrations(databaseURL, migrationsPath); err != nil {
			return nil, nil, nil, nil, noop, err
		}

		pool, err := postgres.NewPool(context.Background(), databaseURL)
		if err != nil {
			return nil, nil, nil, nil, noop, err
		}

		stockRepo = postgres.NewStockRepo(pool)
		locationRepo = postgres.NewLocationRepo(pool)
		reservationRepo = postgres.NewReservationRepo(pool)
		defaultPub = postgres.NewEventPublisher(pool)
		closeRepos = pool.Close
	}

	if !strings.EqualFold(eventPublisher, "kafka") {
		return stockRepo, locationRepo, reservationRepo, defaultPub, closeRepos, nil
	}

	brokers := strings.Split(getenv("KAFKA_BROKERS", "localhost:9092"), ",")
	writer := kafkaadapter.NewWriter(brokers...)
	logger.Info("event publisher configured", "publisher", "kafka", "topic", kafkaadapter.Topic, "brokers", brokers)

	closeAll := func() {
		if err := writer.Close(); err != nil {
			logger.Error("error closing kafka writer", "error", err)
		}
		closeRepos()
	}

	return stockRepo, locationRepo, reservationRepo, kafkaadapter.NewPublisher(writer, reservationRepo), closeAll, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
