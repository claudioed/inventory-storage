-- Inventory Flow & Accuracy analytics read model (ADR-0011).
--
-- This is the ANALYTICAL database, separate from the OLTP database. It is
-- written only by cmd/inventory-projector and read (read-only) by
-- cmd/inventory-reports. The tables here are projections derived from the
-- analytics event stream, not sources of truth.

-- Idempotency + freshness: every applied analytics event id is recorded here
-- exactly once. applied_at is wall-clock insert time; occurred_at is the
-- event's business time, used to compute the projection's freshness lag.
CREATE TABLE analytics_processed_events (
    event_id    TEXT PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_analytics_processed_events_occurred_at
    ON analytics_processed_events (occurred_at DESC);

-- Consumer-level dedupe set, used by the inbound consumer's
-- report.ProcessedEvents gate. It is kept SEPARATE from
-- analytics_processed_events (which the projection UPSERT claims) so the two
-- idempotency layers do not race to claim the same event_id: the consumer
-- gate admits the event, the projection then records its effect.
CREATE TABLE analytics_consumed_events (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The Inventory Flow & Accuracy rollup fact table: one row per
-- (sku, bin_id, hour_bucket). Flow events (received, picked, reserved) carry a
-- SKU but no bin; accuracy events (cycle count, discrepancy) carry a bin but
-- no SKU; stow and unlocate carry both. The unused dimension is the empty
-- string for that event. Counters and summed quantities are UPSERTed as events
-- arrive.
CREATE TABLE flow_accuracy_rollup (
    sku                    TEXT NOT NULL,
    bin_id                 TEXT NOT NULL,
    hour_bucket            TIMESTAMPTZ NOT NULL,
    received_quantity      BIGINT NOT NULL DEFAULT 0,
    stowed_count           BIGINT NOT NULL DEFAULT 0,
    picked_quantity        BIGINT NOT NULL DEFAULT 0,
    reservations_created   BIGINT NOT NULL DEFAULT 0,
    reservations_expired   BIGINT NOT NULL DEFAULT 0,
    reservations_revoked   BIGINT NOT NULL DEFAULT 0,
    cycle_counts_completed BIGINT NOT NULL DEFAULT 0,
    discrepancies_detected BIGINT NOT NULL DEFAULT 0,
    unlocated_count        BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (sku, bin_id, hour_bucket)
);

CREATE INDEX idx_flow_accuracy_rollup_hour_bucket
    ON flow_accuracy_rollup (hour_bucket);
