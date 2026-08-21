# Build Tasks — Inventory & Storage

Build the full bounded context described in CLAUDE.md, in order. Keep
`go build ./...` and `go test ./...` green throughout. Read
/Users/claudioed/docs/amazon-fulfillment-ddd.md for the domain model first.

## Task 0 — Skeleton
- `go mod init github.com/claudioed/inventory-storage`; create the layout;
  .gitignore (bin/, .env); add chi + pgx deps.

## Task 1 — Domain (pure Go)
- shared: SKU, BinId, Quantity value objects; DomainEvent + the 10 events.
- location: Bin aggregate (capacity, occupancy; reject stow when full).
- stock: StockUnit aggregate (qty>=0, states, stow needs item+location, Unlocated).
- reservation: Reservation aggregate (revocable, timeout, <= usable, no double-consume).
- Unit tests for EVERY invariant incl. failing paths.

## Task 2 — Application
- ports: StockRepo, LocationRepo, ReservationRepo, EventPublisher, Clock.
- usecases: the 7 use cases from CLAUDE.md, depending only on domain + ports.
- Unit-test against in-memory adapter. Compute usable = on-hand - active reservations.

## Task 3 — Outbound adapters
- memory: thread-safe impls of every port.
- postgres: pgxpool repos + migrations (stock_unit, bin, reservation, events);
  build-tagged integration test (skip w/o DATABASE_URL).
- events: log/buffered publisher, kafka-ready interface.

## Task 4 — Inbound HTTP
- chi router + handlers for every endpoint, DTOs, domain-error->HTTP mapping, /healthz.
- httptest per endpoint against in-memory repos.

## Task 5 — Composition & ops
- cmd/inventory/main.go wires env -> adapters -> use cases -> router.
- docker-compose.yml (pg16); README.md with run steps + curl examples.

## Task 6 — Verify
- build, vet, test (and `-race`) all green; gofmt clean. Confirm the four named
  invariants each have a red-path test. Do not stop until DoD in CLAUDE.md is met.

## Task 7 — Cross-service integration (additive, see CLAUDE.md's new section)
- Add `github.com/segmentio/kafka-go` dependency.
- New Kafka outbound publisher adapter implementing the existing EventPublisher
  port, publishing StockReserved and ReservationRevoked to
  warehouse.inventory.events, selected via EVENT_PUBLISHER env (default "log").
- Unit test the envelope shape the adapter produces.
- README gains an Integration section. REAL smoke test against the shared
  broker (docker-compose.kafka.yml in ~/warehouse-systems, localhost:9092):
  call POST /reservations with EVENT_PUBLISHER=kafka and confirm the message
  is consumable on the topic before declaring done.
- Full existing suite (build/vet/test/-race) must still be green afterward.
