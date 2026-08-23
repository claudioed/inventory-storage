---
title: Architecture
sidebar_label: Architecture
description: Hexagonal / ports-and-adapters layering, the strict dependency rule, and how it is enforced.
---

# Architecture

The service is **hexagonal (ports & adapters)** with one non-negotiable rule:

> **domain depends on nothing; application depends on domain; adapters depend
> on application/domain.**

No framework type, no `chi` router, no `pgx` connection, and no SQL string ever
appears inside `internal/domain/`.

## Layout

```text
cmd/inventory/               main.go — composition root
internal/
  domain/                    pure Go — no framework, no SQL types
    location/                Bin aggregate (capacity, occupancy)
    stock/                   StockUnit aggregate (SKU@bin, qty, state)
    reservation/             Reservation aggregate (revocable, timeout)
    shared/                  SKU, BinId, Quantity value objects; domain events
  application/
    ports/                   OUT interfaces: StockRepo, LocationRepo,
                             ReservationRepo, EventPublisher, Clock
    usecases/                one struct per use case
  adapters/
    inbound/http/            chi handlers, DTOs, RFC 7807 error mapping
    outbound/postgres/       pgxpool repos + golang-migrate migrations
    outbound/memory/         thread-safe in-memory repos (tests, local dev)
    outbound/events/         log publisher + buffered publisher
    outbound/kafka/          integration-event publisher (kafka-go)
  architecture/              arch-go fitness tests encoding the rule above
migrations/                  golang-migrate SQL files
apis/                        openapi.yaml + asyncapi.yaml (Spectral-linted)
features/                    Gherkin acceptance specs (godog)
charts/inventory-storage/    Helm chart
```

## The dependency flow

```mermaid
flowchart TB
  HTTP["inbound/http<br/>chi handlers, DTOs"]
  UC["application/usecases<br/>ReceiveStock, StowStock, ReserveStock, …"]
  P["application/ports<br/>StockRepo · LocationRepo · ReservationRepo<br/>EventPublisher · Clock"]
  D["domain<br/>stock · location · reservation · shared"]
  PG["outbound/postgres"]
  MEM["outbound/memory"]
  EV["outbound/events"]
  KAF["outbound/kafka"]

  HTTP --> UC
  UC --> P
  UC --> D
  P --> D
  PG -.implements.-> P
  MEM -.implements.-> P
  EV -.implements.-> P
  KAF -.implements.-> P

  classDef dom fill:#0f766e,stroke:#134e4a,color:#fff;
  classDef app fill:#2563eb,stroke:#1e3a8a,color:#fff;
  classDef adp fill:#64748b,stroke:#334155,color:#fff;
  class D dom;
  class UC,P app;
  class HTTP,PG,MEM,EV,KAF adp;
```

Solid arrows are compile-time imports; dashed arrows are interface
satisfaction. Note that **no arrow points from the application layer into an
adapter** — the application only ever names an interface in
`application/ports`, and `cmd/inventory/main.go` is the only place that decides
which implementation gets plugged in.

## The rule is executable, not aspirational

`internal/architecture/architecture_test.go` uses
[arch-go](https://github.com/arch-go/arch-go) to assert the layering as a
normal Go test, and CI runs it as a blocking `arch-test` job. The rules it
encodes:

1. `internal/domain/...` must not import any other `internal/...` package.
2. `internal/application/...` must not import `internal/adapters/...`.
3. `internal/adapters/inbound/...` must not import
   `internal/adapters/outbound/...` (and vice versa).
4. Only `cmd/...` may import adapters *and* application together.

If someone reaches from a use case into `pgx` for a "quick fix," the build goes
red. See [ADR 0006](/docs/adr/0006-arch-go-fitness-tests).

## Ports

| Port | Responsibility | Implementations |
| --- | --- | --- |
| `StockRepo` | Persist/retrieve `StockUnit`; find by ID, SKU, bin; mint IDs | `postgres`, `memory` |
| `LocationRepo` | Persist/retrieve `Bin` | `postgres`, `memory` |
| `ReservationRepo` | Persist/retrieve `Reservation`; mint IDs | `postgres`, `memory` |
| `EventPublisher` | Publish a `shared.DomainEvent` | `events` (log/buffered), `postgres` (event table), `kafka` |
| `Clock` | `Now()` — makes reservation timeouts deterministic in tests | `memory.SystemClock`, fixed clocks in tests |

The `Clock` port matters more than it looks: reservation expiry is a *domain*
concept, so time must be injected rather than read from `time.Now()` inside an
aggregate. Every domain event's `occurredAt` comes from that injected clock.

## Composition root

`cmd/inventory/main.go` is the only file that reads environment variables and
the only file that knows both an interface and its implementation:

| Env var | Default | Effect |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Listen address |
| `DATABASE_URL` | *(unset)* | If unset, the in-memory adapters are used and no database is required. If set, migrations run and the Postgres adapters are wired. |
| `MIGRATIONS_PATH` | `migrations` | Where golang-migrate looks for SQL files |
| `EVENT_PUBLISHER` | `log` | `kafka` swaps in the integration-event publisher |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker list |

The in-memory fallback is deliberate: `go run ./cmd/inventory` with no
environment at all starts a fully functional service, which is what makes the
`httptest` suite and the godog acceptance suite cheap to run.

## Quality gates

Everything below runs in `.github/workflows/ci.yml` on every push and pull
request:

| Job | What it enforces |
| --- | --- |
| `lint` | `golangci-lint` against the committed `.golangci.yml` |
| `test` | Unit tests with `-race`; coverage gate on domain + application |
| `bdd` | godog acceptance suite over the Gherkin specs in `features/` |
| `integration` | Real Postgres 16 service container; every outbound Postgres adapter exercised against it |
| `mutation` | `gremlins` on `internal/domain/...` (scheduled/dispatch only, never blocking) |
| `api-lint` | Spectral against `apis/openapi.yaml` **and** `apis/asyncapi.yaml` |
| `helm-lint` | `ct lint` against `charts/inventory-storage` |
| `arch-test` | The hexagonal fitness tests above |
| `docker-publish` | Gated on all of the above; pushes to Docker Hub on `main` only |

This documentation site is built and deployed by a separate workflow,
`.github/workflows/docs.yml`, which never touches `ci.yml`.
