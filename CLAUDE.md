# Project: Inventory & Storage (Core Bounded Context)

The WMS-tier authoritative record of **what is held where, and what portion is
usable**. It implements Amazon-style **chaotic (random) stow**: no fixed product
location — an item goes to any free bin, and the system records the exact bin. It
supplies "stock reality" to Work Planning (the WES core) and must make allocation
a **revocable reservation** so physical delivery failures never strand an order.

Source of truth for the domain model: `/Users/claudioed/docs/amazon-fulfillment-ddd.md`
and `/Users/claudioed/warehouse-systems-ddd.md`. Honor that ubiquitous language.

## Architecture (NON-NEGOTIABLE)

Hexagonal / Ports & Adapters. Strict dependency rule: **domain depends on
nothing; application depends on domain; adapters depend on application/domain.**
No framework or SQL types in the domain layer.

```
cmd/inventory/               main.go — composition root
internal/
  domain/
    location/                Bin/Location aggregate (capacity, occupancy)
    stock/                   StockUnit aggregate (SKU@location, qty, state)
    reservation/             Reservation aggregate (revocable, timeout)
    shared/                  value objects: SKU, BinId, Quantity, events
  application/
    ports/                   OUT interfaces: StockRepo, LocationRepo, ReservationRepo, EventPublisher, Clock
    usecases/                one struct per use case
  adapters/
    inbound/http/            chi handlers, DTOs, error mapping
    outbound/postgres/       pgxpool repos + migrations
    outbound/memory/         in-memory repos for tests/local
    outbound/events/         log/buffered publisher (kafka-ready iface)
migrations/                  golang-migrate SQL files
```

## Analytics data product (ADR-0011)

Additive read side built from this service's OWN domain events. The OLTP
domain/application layers are NOT modified and must NOT import the analytics
store (arch-test enforces). `internal/analytics/report/` depends on nothing.

- Events are fanned to a SEPARATE topic `warehouse.inventory.analytics` by a new
  outbound adapter; the integration topic/publisher are untouched. Selected by
  `EVENT_PUBLISHER=kafka` (fan-out alongside the integration publisher).
- Separate analytical Postgres (`ANALYTICS_DATABASE_URL`), own migrations
  (`migrations/analytics/`), read-only reader role.
- Three processes: `cmd/inventory` (OLTP), `cmd/inventory-projector` (the ONLY
  writer; consumes the analytics topic from FirstOffset, idempotent on
  event_id), `cmd/inventory-reports` (read-only reader, `GET /reports/...`).
  Report exposed via the MCP server too.
- Report: **Inventory Flow & Accuracy**, keyed per SKU/bin × hour (received/
  picked quantity, stow/reservation/cycle-count/discrepancy counts).
- `GET /reports/.../freshness` reports projection lag.

## Ubiquitous Language (use these exact names)

- **StockUnit** — a quantity of a SKU at a specific Bin. Every physical item has
  exactly one known bin OR is flagged Unlocated (lost). This is the core rule.
- **Bin / Location** — a coded slot. Chaotic storage: any SKU may occupy any free
  bin; capacity must not be exceeded.
- **Stow** — placing inbound stock into a bin. INVALID without BOTH an item-scan
  and a location-scan (this is precisely how inventory gets lost if skipped).
- **Usable inventory** — stock immediately available to fulfil (on-hand minus
  active reservations minus held/damaged). Usable, not total, is what constrains
  release. Expose this explicitly.
- **Reservation** — a REVOCABLE binding of a quantity to demand, with a timeout.
  Physical delivery can fail (pod blocked, tote lost, chute jam, short pick), so
  a reservation must be releasable and re-allocatable against a different holding.
- **Cycle count** — verify a bin's contents; reconcile discrepancies; may flag
  Unlocated.
- **ProductClassification** — SKU-level master data (independent of any bin):
  a closed set of `HandlingTag`s (`Hazmat`, `Fragile`, `TemperatureSensitive`,
  `Oversized`, `HighValue`) plus a `TemperatureClass` (`Ambient`/`Chilled`/
  `Frozen`), required only when `TemperatureSensitive` is set. This service
  is the source of truth. Unclassified SKUs carry no constraints (fail-open).
- **HandlingTag** — one of the five closed classification values above. Not
  an open tag set like `facility-layout`'s `LocationType` — these carry real
  regulatory/physical meaning, so the enum is deliberately closed.
- **DOTHazardClass** — an optional US DOT hazard class (1-9, grounded in
  49 CFR §177.848), meaningful only when `HandlingTag.Hazmat` is set but
  still OPTIONAL even then (nullable, for backward compat with already-
  classified hazmat SKUs that predate this field). Drives the same-bin
  segregation check below. See ADR-0010 for the derived 9×9 class-level
  incompatibility matrix and its 4 documented simplification rules
  (division→class collapse, X-and-O both treated as incompatible for a
  single bin, Class 1 maximally restrictive, Class 9 broadly compatible).

## Aggregates & invariants (enforce in domain, unit-tested)

- **StockUnit**: quantity >= 0; a stow requires item + location; state transitions
  Available -> Reserved -> Picked/Removed, or -> Unlocated. No negative usable.
- **Bin/Location**: sum(stock qty in bin) <= capacity; a full bin rejects stow.
- **Reservation**: reserved qty <= usable qty at reserve time; expires after
  timeout; revoke() returns quantity to usable; cannot double-consume.
- **ProductClassification**: `TemperatureSensitive` requires a non-empty,
  valid `TemperatureClass`; absence of `TemperatureSensitive` means
  `TemperatureClass` must be empty. `DOTHazardClass` (1-9) is optional even
  when `Hazmat` is set. `StowStock` enforces placement rules for classified
  SKUs by reading the target bin's zone attributes from `facility-layout`
  (Hazmat SKU requires a hazmat-rated zone; `TemperatureSensitive` SKU
  requires a matching zone `TemperatureClass`) — see ADR-0009. It ALSO
  enforces same-bin DOT segregation (ADR-0010): a hazmat SKU with a
  `DOTHazardClass` is rejected if the target bin already holds a SKU whose
  class is `Incompatible` per the 9×9 matrix — this check is purely LOCAL
  (StockRepo + ProductClassificationRepo, no cross-context call). **Fail-open**
  for unclassified SKUs, unknown/unmodeled bins, and unclassified occupants;
  **fail-closed** only when a classified SKU's zone lookup genuinely fails
  (`ErrLocationClassificationUnavailable`).
- Read models (usable-by-SKU, bin occupancy) are PROJECTIONS from events.

## Domain events (past tense)

StockReceived, ItemStowed, LocationRecorded, StockReserved, ReservationExpired,
ReservationRevoked, StockPicked, ItemUnlocated, CycleCountCompleted,
DiscrepancyDetected, ProductClassified.

## Use cases (application layer)

1. ReceiveStock(sku, qty) -> staged stock awaiting stow
2. StowStock(sku, qty, binId) -> validates item+location scan, respects capacity,
   enforces hazmat/temperature placement rules AND same-bin DOT segregation
   for classified SKUs
3. ReserveStock(sku, qty, demandRef) -> revocable Reservation against usable
4. RevokeReservation(reservationId) -> returns qty to usable
5. ConfirmPick(reservationId) -> consumes reservation, StockPicked
6. GetUsable(sku) -> usable-inventory read model
7. RunCycleCount(binId, countedQty) -> reconcile, may raise Discrepancy/Unlocated
8. ClassifyProduct(sku, handlingTags, temperatureClass?, dotHazardClass?) ->
   registers/replaces a SKU's ProductClassification (this service is the
   source of truth)

## REST API (inbound adapter)

- POST /stock/receive                        -> ReceiveStock
- POST /stock/stow                           -> StowStock
- POST /reservations                         -> ReserveStock
- DELETE /reservations/{id}                  -> RevokeReservation
- POST /reservations/{id}/confirm-pick       -> ConfirmPick
- GET  /inventory/{sku}/usable               -> GetUsable
- POST /bins/{binId}/cycle-count             -> RunCycleCount
- PUT  /products/{sku}/classification        -> ClassifyProduct
- GET  /products/{sku}/classification        -> current ProductClassification
- GET  /healthz

JSON DTOs live in the http adapter; never leak domain structs.

## Tech & standards

- Go 1.26, modules. Module path: `github.com/claudioed/inventory-storage`.
- chi (github.com/go-chi/chi/v5), pgx/v5 + pgxpool, golang-migrate SQL migrations.
- Config via env (DATABASE_URL, HTTP_ADDR). docker-compose.yml for Postgres 16.
- Typed domain errors mapped to HTTP status in the adapter.
- Table-driven tests: domain + application (in-memory adapter); one httptest per
  endpoint; build-tagged Postgres integration test (skipped w/o DATABASE_URL).
- gofmt/go vet clean; every package has a doc comment.

## Local quality gate (run before every commit)

- After making changes and **before committing**, run `make check`. That is the
  fast self-correction loop: `fmt-check`, `vet`, `build`, `lint`, `test`
  (`go test ./... -race`). It needs no database and finishes in about a minute.
- **Before pushing**, run `make check-all` — `check` plus the 90% `coverage`
  gate, `arch-test` (hexagonal fitness) and `bdd` (godog/Gherkin acceptance).
- Run `make vuln` (`govulncheck ./...`) after touching `go.mod`/`go.sum`; it is
  a blocking CI job and it flags known CVEs in the dependency graph and stdlib.
- `make mutation` runs the fast gremlins subset that blocks in CI
  (`./internal/domain/stock`, thresholds in `.gremlins.yaml`);
  `make mutation-full` is the exhaustive scheduled run over `./internal/domain`.
- `make integration` needs a running Postgres and `DATABASE_URL`; it is
  deliberately outside `check`/`check-all`.
- The lefthook git hooks enforce this automatically once you have run
  `lefthook install` locally (pre-commit: fmt-check/vet/lint; pre-push:
  `make check`) — but run `make check` proactively rather than relying on the
  hook, since hooks are per-clone and may not be installed.
- Why: it keeps quality *left* (harness engineering) — the CI sensors are
  available locally so problems are caught and self-corrected before they ever
  reach a human reviewer or the pipeline.

## Definition of done

- `go build ./...`, `go vet ./...`, `go test ./...` all green.
- README.md: run steps (compose/migrate/go run), endpoints w/ curl, layering note.
- These invariants each have a failing-path test: bin-capacity rejection,
  stow-requires-item-and-location, reservation <= usable, revoke returns to usable.

---

## Cross-service integration (additive — Task 7, do NOT touch existing domain code)

This service PUBLISHES integration events over Kafka to a shared broker. This
round it does not need to consume anything. Strictly additive: new adapter only,
no change to existing aggregates, invariants, or use cases above.

### Envelope (identical across all four warehouse-systems services)

```json
{
  "event_id": "uuid-v4",
  "event_type": "StockReserved",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "inventory-storage",
  "data": { }
}
```

### Kafka

- Client library: `github.com/segmentio/kafka-go`.
- Broker: `KAFKA_BROKERS` env var (default `localhost:9092`). A shared broker
  already runs via `~/warehouse-systems/docker-compose.kafka.yml` — connect to
  it, do not add your own Kafka service to this repo's docker-compose.yml.
- New adapter package `internal/adapters/outbound/kafka/` implementing the
  existing `ports.EventPublisher` interface. Select via env
  (`EVENT_PUBLISHER=kafka|log`, default `log` so existing tests are unaffected).
- Topic: `warehouse.inventory.events`.
- Publish `StockReserved` when `ReserveStock` succeeds:
  `data`: `{"sku": "...", "quantity": N, "demand_ref": "..."}`.
- Publish `ReservationRevoked` when `RevokeReservation` succeeds:
  `data`: `{"sku": "...", "quantity": N, "demand_ref": "..."}`.
  (Both events already exist in your domain event list — just make sure the
  Kafka publisher adapter carries them through with this exact `data` shape;
  do not invent new event names.)

Downstream consumer: wes-work-planning projects these into its own
`UsableInventoryObserved` read model, by SKU.

### Definition of done for Task 7

- New Kafka publisher adapter compiles and is unit-tested (e.g. against an
  in-memory kafka-go writer fake, or by asserting the envelope shape produced).
- Existing full suite (`go build ./...`, `go vet ./...`, `go test ./...`,
  `go test ./... -race`) still green, unchanged.
- README gains an "Integration" section: topic published, exact JSON schemas
  above, the `KAFKA_BROKERS`/`EVENT_PUBLISHER` env vars.
- Do a REAL smoke test: with the shared broker running and `EVENT_PUBLISHER=kafka`,
  actually call `POST /reservations` against the running binary and confirm the
  message lands on `warehouse.inventory.events` via
  `kafka-console-consumer.sh --from-beginning` (or an equivalent one-off Go
  consumer) before declaring done.
