# Inventory & Storage

> **⚠️ Study project.** This repository is an educational exercise in
> Domain-Driven Design applied to warehouse management/execution systems. It
> follows real industry-standard patterns and terminology (WMS/WES/WCS,
> chaotic storage, CloudEvents, RFC 7807, hexagonal architecture) but is
> **not a production system** and is **not affiliated with, endorsed by, or
> representative of Amazon or any other company**.

The WMS-tier authoritative record of **what is held where, and what portion
is usable**. Implements Amazon-style **chaotic (random) stow**: no fixed
product location — an item goes to any free bin, and the system records the
exact bin. Supplies "stock reality" to Work Planning and makes allocation a
**revocable reservation** so a failed physical delivery never strands an
order.

## Documentation

Full documentation site: **https://claudioed.github.io/inventory-storage/**

Business context and domain vision, the DDD model (subdomain classification,
aggregates and invariants, domain events, use cases), an API reference
generated from `apis/openapi.yaml` plus a hand-authored Events page from
`apis/asyncapi.yaml`, the ecosystem context map, and the Architecture Decision
Records. Source lives in [`docs/`](docs/) (Docusaurus); it is built and
deployed to GitHub Pages by [`.github/workflows/docs.yml`](.github/workflows/docs.yml).

## Layering (hexagonal / ports & adapters)

Strict dependency rule: **domain depends on nothing; application depends on
domain; adapters depend on application/domain.**

```
cmd/inventory/               composition root (main.go)
internal/
  domain/                    pure Go — no framework, no SQL types
    location/                Bin aggregate (capacity, occupancy)
    stock/                   StockUnit aggregate (SKU@location, qty, state)
    reservation/              Reservation aggregate (revocable, timeout)
    shared/                  SKU, BinId, Quantity value objects; domain events
  application/
    ports/                   outbound interfaces the application depends on
    usecases/                one struct per use case (ReceiveStock, StowStock, ...)
  adapters/
    inbound/http/            chi router, DTOs, domain-error -> HTTP mapping
    outbound/postgres/       pgxpool repos + golang-migrate migrations
    outbound/memory/         thread-safe in-memory repos (tests, local dev)
    outbound/events/         log publisher + buffered publisher (kafka-ready interface)
    outbound/kafka/          Kafka publisher (integration events, see below)
migrations/                  golang-migrate SQL files
```

The application layer never imports an adapter package — it depends only on
`application/ports` interfaces, which `adapters/outbound/*` implement. The
inbound HTTP adapter never leaks domain structs across the wire; every
response is a DTO.

## Design notes

- **Reservation is SKU-scoped, not bin-scoped.** `ReserveStock` draws from
  whichever `StockUnit`s have usable quantity (first-fit across bins) and
  records exactly which units/quantities it drew from as `Allocation`s on the
  `Reservation`. `RevokeReservation` returns quantity to those same units, but
  because a fresh `ReserveStock` call is free to draw from any unit with
  usable quantity, a subsequent reservation can be satisfied from a
  **different physical holding** — this is what makes a reservation
  revocable without stranding an order when a specific pick fails.
- **StockUnit lifecycle**: `AVAILABLE` -> `RESERVED` (any reserved quantity
  present) -> `PICKED` (physically removed, quantity remains) or `REMOVED`
  (quantity reached zero), or -> `UNLOCATED` (cycle count could not account
  for it). `Usable = on-hand - reserved`, and is zero for `UNLOCATED` /
  `REMOVED` units.
- **ReceiveStock does not create a `StockUnit`.** A `StockUnit` requires both
  a SKU and a Bin (item-scan + location-scan) by construction — that is the
  domain's stow-requires-both invariant. Receiving stages goods (publishes
  `StockReceived`) without persisting an aggregate; the durable record starts
  at `StowStock`.
- **Cycle count shortfall** marks whichever `StockUnit`s cover the shortfall
  fully `UNLOCATED` (not split into located/lost sub-quantities), publishing
  `ItemUnlocated` per unit touched, kept simple by design. An overage is
  reported as a `DiscrepancyDetected`/`CycleCountCompleted(discrepancy=true)`
  pair for a separate receiving/audit process to reconcile.

## Run it

### Option A — in-memory (no database)

```sh
go run ./cmd/inventory
```

Without `DATABASE_URL` set, the app wires the in-memory adapters and logs
events to stdout. Listens on `:8080` (override with `HTTP_ADDR`).

### Option B — Postgres

```sh
docker compose up -d postgres
export DATABASE_URL='postgres://inventory:inventory@localhost:5432/inventory?sslmode=disable'
go run ./cmd/inventory
```

Migrations in `migrations/` run automatically on startup (via
`MIGRATIONS_PATH`, default `migrations`).

## Endpoints

| Method | Path | Use case |
|--------|------|----------|
| POST   | `/stock/receive` | ReceiveStock |
| POST   | `/stock/stow` | StowStock |
| POST   | `/reservations` | ReserveStock |
| DELETE | `/reservations/{id}` | RevokeReservation |
| POST   | `/reservations/{id}/confirm-pick` | ConfirmPick |
| GET    | `/inventory/{sku}/usable` | GetUsable |
| POST   | `/bins/{binId}/cycle-count` | RunCycleCount |
| GET    | `/healthz` | liveness |

### curl walkthrough

```sh
# Stow requires a bin to exist first (there is no "create bin" endpoint yet;
# seed one directly via the Postgres adapter, or run against in-memory and
# call StowStock against a bin your test/seed script created).

curl -s localhost:8080/healthz

curl -s -X POST localhost:8080/stock/receive \
  -d '{"sku":"SKU-1","quantity":10}'
# => 202 Accepted (a staged receipt has no addressable resource yet)

curl -s -i -X POST localhost:8080/stock/stow \
  -d '{"sku":"SKU-1","quantity":10,"binId":"A-1-1"}'
# => 201 Created, Location: /stock/<stock-unit-id>

curl -s -i -X POST localhost:8080/reservations \
  -d '{"sku":"SKU-1","quantity":6,"demandRef":"order-42"}'
# => 201 Created, Location: /reservations/<id>, body {"id":"res-...", ...}

curl -s localhost:8080/inventory/SKU-1/usable

curl -s -X POST localhost:8080/reservations/<id>/confirm-pick

curl -s -X DELETE localhost:8080/reservations/<id>

curl -s -X POST localhost:8080/bins/A-1-1/cycle-count \
  -d '{"countedQuantity":9}'
```

Error responses are RFC 7807 (`application/problem+json`), with a status
mapped from the typed domain/application error (400 missing/malformed
input, 422 well-formed but semantically invalid values like a non-positive
quantity, 404 not found, 409 conflict — e.g. bin full, reservation exceeds
usable, reservation already resolved):

```sh
curl -s -i -X DELETE localhost:8080/reservations/does-not-exist
# HTTP/1.1 404 Not Found
# Content-Type: application/problem+json
#
# {
#   "type": "https://errors.inventory-storage.warehouse-systems.dev/reservation-not-found",
#   "title": "Reservation not found",
#   "status": 404,
#   "detail": "reservation not found",
#   "instance": "/reservations/does-not-exist"
# }
```

## Integration

This service publishes integration events to the shared warehouse-systems
Kafka broker so other bounded contexts (e.g. `wes-work-planning`) can project
their own read models from inventory reality. It does not consume anything
yet.

- **Topic**: `warehouse.inventory.events`
- **Publisher selection**: `EVENT_PUBLISHER` env var — `log` (default,
  unchanged behavior: stdout logging with in-memory adapters, or the Postgres
  outbox with `DATABASE_URL` set) or `kafka`.
- **Broker**: `KAFKA_BROKERS` env var, comma-separated, default
  `localhost:9092`. Start the shared broker from the workspace root:
  ```sh
  docker compose -f ~/warehouse-systems/docker-compose.kafka.yml up -d
  ```
- **Envelope** (identical across all warehouse-systems services):
  ```json
  {
    "event_id": "uuid-v4",
    "event_type": "StockReserved",
    "occurred_at": "2026-08-21T22:00:00Z",
    "source": "inventory-storage",
    "data": {}
  }
  ```
- **Events published** — `StockReserved` (on a successful `ReserveStock`) and
  `ReservationRevoked` (on a successful `RevokeReservation`), both with the
  same `data` shape:
  ```json
  {"sku": "SKU-1", "quantity": 4, "demand_ref": "order-42"}
  ```
  (`ReservationRevoked`'s domain event only carries the reservation id; the
  Kafka adapter looks the reservation back up via `ReservationRepo` to fill in
  `sku`/`quantity`/`demand_ref`.) Every other domain event (`StockReceived`,
  `ItemStowed`, ...) is not part of this integration contract and is not
  forwarded to Kafka.

Run it against the real broker:

```sh
export EVENT_PUBLISHER=kafka
export KAFKA_BROKERS=localhost:9092
export DATABASE_URL='postgres://inventory:inventory@localhost:5432/inventory?sslmode=disable'
go run ./cmd/inventory

# in another shell, tail the topic:
docker exec -it warehouse-kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 --topic warehouse.inventory.events --from-beginning

# then drive a reservation + revoke through the API (see curl walkthrough above)
```

## Observability

Traces and metrics are exported over **OTLP/gRPC** to an OpenTelemetry
Collector; logs stay on stdout as JSON and carry the ids that tie them back to
a trace. There is no `/metrics` endpoint — Prometheus exposition is the
Collector's job, not this service's.

### Environment variables

| Variable | Default | What it does |
| --- | --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | Collector's OTLP/gRPC receiver. Accepts a bare `host:port` (plaintext) or a full URL (`https://…` for TLS). |
| `OTEL_SERVICE_NAME` | `inventory-storage` | `service.name` resource attribute, and the span/metric scope name. |
| `SERVICE_VERSION` | `dev` | `service.version` resource attribute. |
| `ENVIRONMENT` | `local` | `deployment.environment.name` resource attribute. |
| `LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error`, case-insensitive. Also gates the OTel SDK's own diagnostics, which are bridged onto the same JSON logger. |

A Collector is *expected* at `OTEL_EXPORTER_OTLP_ENDPOINT`, but is never
required: the exporters dial lazily and no blocking dial option is set, so a
Collector that is down or absent costs telemetry and nothing else. Startup,
request latency and exit code are all unaffected — a failed final flush is
logged at `WARN`, not returned. In the `warehouse-infra` kind cluster the
endpoint points at the in-cluster Collector Service
(`otel-collector.observability.svc.cluster.local:4317`, set by the Helm chart's
`otel` values block).

### What gets exported

**Traces** — one server span per HTTP request, named after the *chi route
pattern* rather than the raw path (`/reservations/{id}`, not one span name per
reservation id), with these as children:

- every Postgres query, prepare, batch, copy and pool acquire, via `otelpgx`.
  Statements are normalized, so bound arguments — SKUs, bin codes, demand
  references — never leave the process as span attributes;
- `kafka.publish warehouse.inventory.events` for each integration event, with
  the W3C `traceparent` injected into the message headers. A consumer that
  extracts from those headers joins *this* trace, which is what makes the
  reserve → release path visible end to end across services.

**Metrics**

- `http.server.request.duration` (histogram, seconds) — OTel HTTP semantic
  conventions, from `otelchi`;
- `inventory.reservations` (counter) — the business signal, with an
  `outcome` attribute of `created` or `revoked`. It is recorded in the
  `ReserveStock` / `RevokeReservation` use cases, not the HTTP handler, so it
  counts reservations that were actually bound and durably saved rather than
  requests that merely arrived;
- Go runtime metrics (goroutines, GC, memory) via
  `contrib/instrumentation/runtime`.

**Logs** stay `log/slog` JSON on stdout. Any record written with a
span-carrying context gains `trace_id` and `span_id`, so a log line pivots
straight to its trace:

```json
{"time":"2026-08-23T20:09:13.9-03:00","level":"INFO","msg":"http request","method":"POST","path":"/reservations","status":201,"duration_ms":52,"trace_id":"0a225d107cf073c4f1f4ea2cadeb2941","span_id":"818a21eae51ab9de"}
```

### Trying it locally

```sh
# a Collector that just prints what it receives
cat > /tmp/otelcol.yaml <<'YAML'
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
exporters:
  debug:
    verbosity: detailed
service:
  pipelines:
    traces:  {receivers: [otlp], exporters: [debug]}
    metrics: {receivers: [otlp], exporters: [debug]}
YAML
docker run --rm -p 4317:4317 -v /tmp/otelcol.yaml:/etc/otelcol-contrib/config.yaml \
  otel/opentelemetry-collector-contrib:latest

# in another shell
go run ./cmd/inventory        # then drive the curl walkthrough above
```

## Local development / quality gate

Every CI sensor is also a `make` target, so the same feedback is available
locally, before you commit. `make help` lists them all.

```sh
make check        # fast pre-commit loop: fmt-check, vet, build, lint, test (-race)
make check-all    # before pushing: check + coverage gate (90%), arch-test, bdd
make vuln         # govulncheck ./... — known CVEs in deps and the Go stdlib
make mutation     # fast gremlins subset (blocks in CI); mutation-full = exhaustive
make integration  # needs a running Postgres + DATABASE_URL (not part of check)
```

Git hooks are managed with [lefthook](https://github.com/evilmartians/lefthook)
and configured in [`lefthook.yml`](lefthook.yml) — `pre-commit` runs
`make fmt-check vet lint`, `pre-push` runs `make check`. Hooks live in
`.git/hooks/`, which is not tracked, so activate them once per clone:

```sh
brew install lefthook     # or: go install github.com/evilmartians/lefthook@latest
lefthook install
```

`make lint` expects `golangci-lint` on your PATH at the version CI pins:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1
go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
go install golang.org/x/vuln/cmd/govulncheck@latest
```

## Tests

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...

# Postgres integration test (build-tagged, skipped without DATABASE_URL)
docker compose up -d postgres
DATABASE_URL='postgres://inventory:inventory@localhost:5432/inventory?sslmode=disable' \
  go test -tags integration ./internal/adapters/outbound/postgres/...
```

Each of the four named invariants has a dedicated failing-path test:

| Invariant | Test |
|-----------|------|
| Bin-capacity rejection | `TestBin_Occupy_ExceedsCapacity_Rejected` (domain), `TestStowStock_ExceedsBinCapacity_Rejected` (use case) |
| Stow requires item + location | `TestNewStockUnit_RequiresSKU` / `_RequiresBin` (domain) |
| Reservation <= usable | `TestStockUnit_Reserve_ExceedsUsable_Rejected` (domain), `TestReserveStock_ExceedsUsable_Rejected` (use case) |
| Revoke returns to usable | `TestStockUnit_ReleaseReservation_ReturnsToUsable` (domain), `TestRevokeReservation_ReturnsQuantityToUsable` (use case) |

## BDD / Acceptance tests

Executable specifications written in Gherkin and run with
[godog](https://github.com/cucumber/godog), the official Cucumber
implementation for Go.

The feature files live under [`features/`](features/) — one per
aggregate/bounded concept, using the ubiquitous language from `CLAUDE.md`
(StockUnit, Bin, Stow, Usable inventory, Reservation, Cycle count):

| Feature file | Covers |
|--------------|--------|
| `features/stow.feature` | `POST /stock/receive`, `POST /stock/stow` — chaotic stow, bin-capacity rejection |
| `features/reservation.feature` | `POST /reservations`, `DELETE /reservations/{id}`, `POST /reservations/{id}/confirm-pick` — reserve against usable, revoke, confirm pick |
| `features/cycle_count.feature` | `POST /bins/{binId}/cycle-count` — clean count vs. discrepancy/Unlocated |
| `features/usable_inventory.feature` | `GET /inventory/{sku}/usable` — on-hand minus active reservations |

The step definitions live in [`features_test.go`](features_test.go) at the repo
root. They are true black-box acceptance tests: the real chi router is wired to
the in-memory outbound adapters, served over `httptest.NewServer`, and driven
with plain `net/http` requests — no use case is called directly. Every scenario
gets a fresh server and fresh state via a godog `Before` hook.

Run them locally:

```sh
go test ./... -run TestFeatures -v
```

They also run as the `bdd` job in CI.
