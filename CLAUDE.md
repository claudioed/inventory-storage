# Project: Inventory & Storage (Core Bounded Context)

The WMS-tier authoritative record of **what is held where, and what portion is
usable**. Implements Amazon-style **chaotic (random) stow**: no fixed product
location — an item goes to any free bin, and the system records the exact bin.
Supplies "stock reality" to Work Planning (the WES core) and makes allocation
a **revocable reservation** so a failed physical delivery never strands an
order.

Source of truth for the domain model: `/Users/claudioed/docs/amazon-fulfillment-ddd.md`
and `/Users/claudioed/warehouse-systems-ddd.md`. Honor that ubiquitous
language everywhere in this repo.

Go module: `github.com/claudioed/inventory-storage` (Go 1.26).

## Project Overview

- Three binaries: `cmd/inventory` (OLTP REST API), `cmd/inventory-projector`
  (analytics writer), `cmd/inventory-reports` (analytics reader). Plus
  `cmd/mcp` (MCP inbound adapter, Streamable HTTP) and `web/` (a standalone
  Vite/React micro-frontend remote — see `.claude/rules/frontend-mfe.md`).
- API contracts are the single source of truth for generated docs:
  `apis/openapi.yaml` (REST, Spectral-linted) and `apis/asyncapi.yaml`
  (events, Spectral-linted). Docs site (Docusaurus) lives in `docs/` and
  regenerates its REST reference from `apis/openapi.yaml` via
  `npm run gen-api-docs` (see Key Commands).
- Detailed reference material that used to live in this file has been split
  out to keep this index scannable — see "Further reading" at the bottom.

## Architecture (NON-NEGOTIABLE)

Hexagonal / Ports & Adapters. Strict dependency rule: **domain depends on
nothing; application depends on domain; adapters depend on application/domain.**
No framework or SQL types in the domain layer.

```
cmd/inventory/               main.go — composition root (OLTP REST API)
cmd/inventory-projector/     analytics WRITER: consumes analytics topic, projects
cmd/inventory-reports/       analytics READER: read-only report REST
cmd/mcp/                     MCP inbound adapter (Streamable HTTP)
internal/
  domain/
    location/                Bin/Location aggregate (capacity, occupancy)
    stock/                   StockUnit aggregate (SKU@location, qty, state)
    reservation/              Reservation aggregate (revocable, timeout)
    product/                  ProductClassification aggregate (SKU master data)
    shared/                  value objects: SKU, BinId, Quantity, events
  analytics/report/          read-model region (depends on nothing) — data product
  application/
    ports/                   OUT interfaces: StockRepo, LocationRepo, ReservationRepo,
                              ProductClassificationRepo, EventPublisher, Clock
    usecases/                one struct per use case
  adapters/
    inbound/http/            chi handlers, DTOs, error mapping
    inbound/kafka/           analytics consumer (projector)
    inbound/mcp/             MCP tools incl. the read-only report tool
    outbound/postgres/       pgxpool repos + migrations
    outbound/memory/         in-memory repos for tests/local
    outbound/events/         log/buffered/multi (fan-out) publisher
    outbound/kafka/          Kafka integration + analytics publishers
    outbound/analyticsstore/ analytical Postgres projection + read-only reader
migrations/                  golang-migrate SQL files (OLTP)
migrations/analytics/        golang-migrate SQL files (analytical read model)
web/                         inventory-mfe — Vite/React MFE remote (separate module)
```

The application layer never imports an adapter package — it depends only on
`application/ports` interfaces. The inbound HTTP adapter never leaks domain
structs across the wire; every response is a DTO.

## Key Commands

Run from the repo root unless noted.

```sh
# Local dev — in-memory adapters, no DB, logs events to stdout
go run ./cmd/inventory                       # listens on :8080 (HTTP_ADDR)

# Local dev — Postgres
docker compose up -d postgres
export DATABASE_URL='postgres://inventory:***@localhost:5432/inventory?sslmode=disable'
go run ./cmd/inventory                       # migrations run automatically

# Quality gate (mirrors .github/workflows/ci.yml — see Testing below)
make check                                   # fast: fmt-check vet build lint test
make check-all                               # check + coverage + arch-test + bdd
make integration                             # needs DATABASE_URL, not in check/check-all
make vuln                                    # govulncheck ./... — after touching go.mod/go.sum
make mutation                                # fast gremlins subset (blocking in CI)
make mutation-full                           # exhaustive gremlins run (scheduled only)

# Docs site (Docusaurus, in docs/)
cd docs && npm ci
npm run gen-api-docs                         # regenerate REST reference from apis/openapi.yaml
npm run build                                # full site build; onBrokenLinks: 'throw'
```

## Code Standards / Testing

- Go 1.26, modules. chi (`go-chi/chi/v5`), pgx/v5 + pgxpool, golang-migrate.
- Config via env (`DATABASE_URL`, `HTTP_ADDR`, `ANALYTICS_DATABASE_URL`,
  `EVENT_PUBLISHER`, `KAFKA_BROKERS`, `CORS_ALLOWED_ORIGINS`,
  `FACILITY_LAYOUT_BASE_URL`, `REPORTS_BASE_URL`). No hardcoded config.
- Typed domain errors mapped to HTTP status (RFC 7807 problem details) in the
  adapter. gofmt/go vet clean; every package has a doc comment.
- Table-driven tests: domain + application (in-memory adapter); one httptest
  per endpoint; build-tagged Postgres integration test (`-tags=integration`,
  skipped without `DATABASE_URL`) — use **testcontainers** for any new Kafka
  integration test, never a skip-gated external broker (fleet-wide rule; CI's
  `integration` job runs Postgres only, no Kafka service).
- **After every change, before committing:** `make check` (fmt-check, vet,
  build, lint, test — ~1 min, no DB).
- **Before pushing:** `make check-all` — adds the 90% coverage gate,
  `arch-test` (hexagonal fitness, enforces the dependency rule above and that
  `internal/analytics/` imports nothing from OLTP domain/application), and
  `bdd` (godog/Gherkin acceptance, `features/*.feature`).
- `make vuln` after touching `go.mod`/`go.sum` — blocking CI job, flags known
  CVEs in the dependency graph and stdlib.
- lefthook git hooks (`lefthook install`) enforce fmt-check/vet/lint
  pre-commit and `make check` pre-push, but run `make check` proactively —
  hooks are per-clone and may not be installed.
- Definition of done: `go build ./...`, `go vet ./...`, `go test ./...` all
  green; README run steps + curl'd endpoints + layering note kept current;
  every invariant below has a failing-path test (bin-capacity rejection,
  stow-requires-item-and-location, reservation <= usable, revoke returns to
  usable).

## Further reading (`.claude/rules/`)

- `domain-model.md` — ubiquitous language, aggregates & invariants, domain
  events, use cases. Read this before touching `internal/domain/` or
  `internal/application/`.
- `rest-api.md` — full REST endpoint table, CORS policy, the
  `demandRef`-scoped read side backing the fleet's Order Lifecycle console.
- `analytics-data-product.md` — ADR-0011: the additive analytics read side,
  its three processes, and the Inventory Flow & Accuracy report.
- `integration-events.md` — the Kafka integration contract: envelope shape,
  topic, what's published today vs. catalog-only (see also
  `apis/asyncapi.yaml` and `docs/docs/api-reference/events.md`, which is the
  generated-adjacent narrative page for the same contract).
- `frontend-mfe.md` — `web/`'s scope and boundary as a Module Federation
  remote.

ADRs for every non-obvious architectural decision live in
`docs/docs/adr/0001..0015` — check there before re-litigating a decision
(e.g. hexagonal layering ADR-0001, chaotic storage ADR-0002, revocable
reservations ADR-0003, DOT hazard segregation ADR-0010, why the REST
identity/bearer-auth layer was added then removed — ADR-0014/0015).
