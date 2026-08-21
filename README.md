# Inventory & Storage

The WMS-tier authoritative record of **what is held where, and what portion
is usable**. Implements Amazon-style **chaotic (random) stow**: no fixed
product location — an item goes to any free bin, and the system records the
exact bin. Supplies "stock reality" to Work Planning and makes allocation a
**revocable reservation** so a failed physical delivery never strands an
order.

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

curl -s -X POST localhost:8080/stock/stow \
  -d '{"sku":"SKU-1","quantity":10,"binId":"A-1-1"}'

curl -s -X POST localhost:8080/reservations \
  -d '{"sku":"SKU-1","quantity":6,"demandRef":"order-42"}'
# => {"id":"su-...", ...}

curl -s localhost:8080/inventory/SKU-1/usable

curl -s -X POST localhost:8080/reservations/<id>/confirm-pick

curl -s -X DELETE localhost:8080/reservations/<id>

curl -s -X POST localhost:8080/bins/A-1-1/cycle-count \
  -d '{"countedQuantity":9}'
```

Error responses are `{"error": "..."}` with a status mapped from the typed
domain/application error (400 invalid input, 404 not found, 409 conflict —
e.g. bin full, reservation exceeds usable, reservation already resolved).

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
