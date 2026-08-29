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
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/facilitylayout"
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

	stockRepo, locationRepo, reservationRepo, classificationRepo, publisher, closeAdapters, err := buildAdapters(databaseURL, migrationsPath, eventPublisher, logger)
	if err != nil {
		return err
	}
	defer closeAdapters()

	reservationMetrics, err := telemetry.NewReservationMetrics()
	if err != nil {
		return err
	}

	clock := memory.SystemClock{}
	locationLookup := buildLocationLookup(getenv("LOCATION_LOOKUP_MODE", "permissive"), os.Getenv("FACILITY_LAYOUT_BASE_URL"), logger)

	server := &inboundhttp.Server{
		ReceiveStock: &usecases.ReceiveStock{Events: publisher, Clock: clock},
		StowStock: &usecases.StowStock{
			Stock: stockRepo, Locations: locationRepo, Events: publisher, Clock: clock,
			Classifications: classificationRepo, LocationLookup: locationLookup,
		},
		ReserveStock:               &usecases.ReserveStock{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock, Metrics: reservationMetrics},
		RevokeReservation:          &usecases.RevokeReservation{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock, Metrics: reservationMetrics},
		ConfirmPick:                &usecases.ConfirmPick{Stock: stockRepo, Locations: locationRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		GetUsable:                  &usecases.GetUsable{Stock: stockRepo},
		GetReservationsByDemandRef: &usecases.GetReservationsByDemandRef{Reservations: reservationRepo},
		RunCycleCount:              &usecases.RunCycleCount{Stock: stockRepo, Events: publisher, Clock: clock},
		ClassifyProduct:            &usecases.ClassifyProduct{Classifications: classificationRepo, Events: publisher, Clock: clock},
		Classifications:            classificationRepo,
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
	ports.StockRepo, ports.LocationRepo, ports.ReservationRepo, ports.ProductClassificationRepo, ports.EventPublisher, func(), error,
) {
	noop := func() {}

	var (
		stockRepo          ports.StockRepo
		locationRepo       ports.LocationRepo
		reservationRepo    ports.ReservationRepo
		classificationRepo ports.ProductClassificationRepo
		defaultPub         ports.EventPublisher
		closeRepos         = noop
	)

	if databaseURL == "" {
		logger.Info("database url not configured; using in-memory adapters")
		stockRepo = memory.NewStockRepo()
		locationRepo = memory.NewLocationRepo()
		reservationRepo = memory.NewReservationRepo()
		classificationRepo = memory.NewProductClassificationRepo()
		defaultPub = events.NewLogPublisher(logger)
	} else {
		if err := postgres.RunMigrations(databaseURL, migrationsPath); err != nil {
			return nil, nil, nil, nil, nil, noop, err
		}

		pool, err := postgres.NewPool(context.Background(), databaseURL)
		if err != nil {
			return nil, nil, nil, nil, nil, noop, err
		}

		stockRepo = postgres.NewStockRepo(pool)
		locationRepo = postgres.NewLocationRepo(pool)
		reservationRepo = postgres.NewReservationRepo(pool)
		classificationRepo = postgres.NewProductClassificationRepo(pool)
		defaultPub = postgres.NewEventPublisher(pool)
		closeRepos = pool.Close
	}

	if !strings.EqualFold(eventPublisher, "kafka") {
		return stockRepo, locationRepo, reservationRepo, classificationRepo, defaultPub, closeRepos, nil
	}

	brokers := strings.Split(getenv("KAFKA_BROKERS", "localhost:9092"), ",")
	writer := kafkaadapter.NewWriter(brokers...)
	integrationPub := kafkaadapter.NewPublisher(writer, reservationRepo)

	// Fan-out: the same domain event is forwarded to BOTH the integration
	// topic (via the untouched integration Publisher) AND the dedicated
	// analytics topic (via a separate AnalyticsPublisher), behind the single
	// ports.EventPublisher the use cases depend on. The integration contract
	// (warehouse.inventory.events) is unchanged; the analytics stream
	// (warehouse.inventory.analytics) evolves independently (ADR-0011).
	analyticsPub := kafkaadapter.NewAnalyticsPublisher(brokers, reservationRepo, nil)
	publisher := events.NewMultiPublisher(integrationPub, analyticsPub)
	logger.Info("event publisher configured", "publisher", "kafka",
		"integration_topic", kafkaadapter.Topic, "analytics_topic", kafkaadapter.AnalyticsTopic, "brokers", brokers)

	closeAll := func() {
		if err := writer.Close(); err != nil {
			logger.Error("error closing kafka integration writer", "error", err)
		}
		if err := analyticsPub.Close(); err != nil {
			logger.Error("error closing kafka analytics writer", "error", err)
		}
		closeRepos()
	}

	return stockRepo, locationRepo, reservationRepo, classificationRepo, publisher, closeAll, nil
}

// buildLocationLookup selects the outbound LocationClassificationLookup
// adapter via LOCATION_LOOKUP_MODE (http|permissive), defaulting to
// "permissive" so existing tests, CI and deployments that do not set the
// env var are unaffected — mirroring the EVENT_PUBLISHER=kafka|log
// pattern. "http" requires FACILITY_LAYOUT_BASE_URL.
func buildLocationLookup(mode, facilityLayoutBaseURL string, logger *slog.Logger) ports.LocationClassificationLookup {
	if !strings.EqualFold(mode, "http") {
		return facilitylayout.NewPermissiveLookup()
	}
	logger.Info("location classification lookup configured", "mode", "http", "facility_layout_base_url", facilityLayoutBaseURL)
	return facilitylayout.NewClient(facilityLayoutBaseURL, nil)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
