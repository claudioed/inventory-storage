---
title: Integration Guide
sidebar_label: Integration Guide
description: How to integrate with inventory-storage — topic, envelope, payloads, configuration, and how to verify it end to end.
---

# Integration Guide

Everything a downstream team needs to consume this service, and everything an
operator needs to run it against the shared broker.

## What this service publishes

| | |
| --- | --- |
| **Topic** | `warehouse.inventory.events` |
| **Events** | `StockReserved`, `ReservationRevoked` |
| **Client library** | `github.com/segmentio/kafka-go` |
| **Balancer** | `LeastBytes`, `AllowAutoTopicCreation: true` |
| **Consumers today** | `wes-work-planning` |

## What this service consumes

**No Kafka topic.** There is no inbound Kafka adapter in this repository.
Everything that changes state here arrives as an explicit HTTP command
against [the REST API](/docs/api-reference), which runs this service's own
invariants before anything is written.

**One synchronous HTTP dependency, added in ADR 0009.** `StowStock` reads
`facility-layout`'s `GET /locations/{locationCode}/classification` when the
SKU being stowed carries the `Hazmat` or `TemperatureSensitive` handling tag,
to enforce placement rules (hazmat-rated zone, matching temperature class).
See [ADR 0009](/docs/adr/0009-product-classification-as-sku-master-data) for
the full design, including the fail-open/fail-closed asymmetry.

| Env var | Default | Purpose |
| --- | --- | --- |
| `LOCATION_LOOKUP_MODE` | `permissive` | `http` calls facility-layout for real; `permissive` (default) is a no-op that always permits the stow — existing tests/CI/deployments unaffected until opted in. |
| `FACILITY_LAYOUT_BASE_URL` | *(unset)* | Base URL for the `http` mode client, e.g. `http://facility-layout:8080`. |

## Configuration

| Env var | Default | Purpose |
| --- | --- | --- |
| `EVENT_PUBLISHER` | `log` | `kafka` swaps `ports.EventPublisher` for the Kafka adapter. The default is `log` so tests and local runs need no broker. |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker list |

The shared broker runs from `~/warehouse-systems/docker-compose.kafka.yml` —
this repository's own `docker-compose.yml` deliberately does **not** define a
Kafka service, so five services do not race to bind the same port.

```bash
# from ~/warehouse-systems
docker compose -f docker-compose.kafka.yml up -d

# from this repo
EVENT_PUBLISHER=kafka KAFKA_BROKERS=localhost:9092 go run ./cmd/inventory
```

## Message shapes

The full envelope discussion — the CloudEvents target, the legacy flat
envelope actually emitted today, and the mapping between them — is on the
[Events page](/docs/api-reference/events). What lands on the topic right now:

```json
{
  "event_id": "1f7a4c30-9b2d-4e85-a6c1-7d3f0b5e8a94",
  "event_type": "StockReserved",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "inventory-storage",
  "data": { "sku": "SKU-1", "quantity": 5, "demand_ref": "order-42" }
}
```

```json
{
  "event_id": "4b9e2f61-7c3a-4d08-85e2-1a6f9c0d3b72",
  "event_type": "ReservationRevoked",
  "occurred_at": "2026-08-21T22:10:00Z",
  "source": "inventory-storage",
  "data": { "sku": "SKU-1", "quantity": 5, "demand_ref": "order-42" }
}
```

Both carry the identical `data` shape, deliberately: the downstream projection
applies one to decrement and the other to increment the same counter.

## The downstream projection

`wes-work-planning` turns these two events into `UsableInventoryObserved`,
a read model **keyed by SKU** (package `internal/domain/inventoryview/`,
table `usable_inventory_view`, exposed at `GET /inventory-view/{sku}`).

```mermaid
sequenceDiagram
    autonumber
    participant C as Client (WES or operator)
    participant I as inventory-storage
    participant K as Kafka<br/>warehouse.inventory.events
    participant W as wes-work-planning

    C->>I: POST /reservations {sku, quantity, demandRef}
    I->>I: sum usable across StockUnits<br/>reserve first-fit, record Allocations
    I->>K: StockReserved {sku, quantity, demand_ref}
    I-->>C: 201 Created + Location
    K->>W: consume
    W->>W: dedupe on event_id (processed_events)
    W->>W: UsableInventoryObserved[sku] -= quantity

    Note over C,I: the physical pick fails
    C->>I: DELETE /reservations/{id}
    I->>I: Revoke(); release quantity back to each StockUnit
    I->>K: ReservationRevoked {sku, quantity, demand_ref}
    I-->>C: 204 No Content
    K->>W: consume
    W->>W: UsableInventoryObserved[sku] += quantity
```

Two design notes that a consumer must respect:

- **Keyed by SKU, not by path.** Inventory reservations are SKU-scoped;
  `wes-work-planning`'s own `CLAUDE.md` calls out explicitly that a path
  mapping must not be forced onto them because it does not exist.
- **Idempotency is mandatory.** Kafka delivery is at-least-once.
  `wes-work-planning` inserts each `event_id` into a `processed_events` table
  before applying the effect, and skips on a primary-key violation — without
  that, one redelivery double-decrements observed usable.

## Consuming this topic yourself

If you are building a sixth consumer:

1. **Read the spec, not this page.**
   [`apis/asyncapi.yaml`](https://github.com/claudioed/inventory-storage/blob/main/apis/asyncapi.yaml)
   is the contract and is Spectral-linted in CI.
2. **Only two events are on the wire.** The AsyncAPI document is the complete
   *catalog*; eight of its ten messages are in-process only and each says so in
   its own description. Do not build against one of those until it is wired.
3. **Tolerate unknown `type` / `event_type` values.** The catalog grows.
4. **Deduplicate on the event id.** At-least-once.
5. **Do not assume ordering.** No partition key is set today.
6. **Treat the events as a projection, not as truth.** For an authoritative
   answer, call `GET /inventory/{sku}/usable`. The event stream exists so
   consumers can keep a cheap local view warm, not so they can reimplement the
   ledger.

## Verifying end to end

The integration was smoke-tested for real against the shared broker, not just
unit-tested. To repeat it:

```bash
# 1. shared broker up
docker compose -f ~/warehouse-systems/docker-compose.kafka.yml up -d

# 2. service up, publishing to Kafka
EVENT_PUBLISHER=kafka go run ./cmd/inventory &

# 3. seed a bin + stow, then reserve
curl -s -X POST localhost:8080/reservations \
  -H 'Content-Type: application/json' \
  -d '{"sku":"SKU-1","quantity":5,"demandRef":"order-42"}'

# 4. confirm it landed
kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic warehouse.inventory.events \
  --from-beginning
```

The unit-level equivalent lives in
`internal/adapters/outbound/kafka/publisher_test.go`, which asserts the exact
envelope the adapter produces against an in-memory `Writer` fake — no broker
required, which is why it runs in the default `go test ./...` suite.

## Deployment

The service ships as a container (`Dockerfile` at the repo root, published to
Docker Hub by the `docker-publish` CI job on `main`) and as a Helm chart
(`charts/inventory-storage`, linted by the `helm-lint` job). In the local
Kubernetes stack it sits behind Kong for north-south traffic and inside the
Istio mesh for east-west; both `EVENT_PUBLISHER` and `KAFKA_BROKERS` are
ordinary chart values.
