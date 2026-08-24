---
title: Domain Events
sidebar_label: Domain Events
description: The ten past-tense domain events this context raises, which aggregate raises each, and which reach the broker.
---

# Domain Events

Eleven past-tense events, raised by four aggregates. Every event carries an
`occurredAt` taken from the injected `Clock` port — never wall-clock time at
publish — so ordering is a domain fact rather than an infrastructure artefact.

```go
// internal/domain/shared/events.go
type DomainEvent interface {
	EventName() string
	OccurredAt() time.Time
}
```

The domain never depends on the publishing mechanism. Use cases hand events to
`ports.EventPublisher`; which adapter is behind it (log, buffered, Postgres
table, Kafka) is a composition-root decision.

## The catalog

| Event | Raised by | Raised when | Payload |
| --- | --- | --- | --- |
| `StockReceived` | StockUnit *(pre-location)* | `ReceiveStock` acknowledges inbound goods | `sku`, `quantity` |
| `ItemStowed` | StockUnit | `StowStock` succeeds — item-scan + location-scan both present | `sku`, `binId`, `quantity` |
| `LocationRecorded` | StockUnit | Immediately after `ItemStowed`; the bin now authoritatively holds this unit | `stockUnitId`, `binId` |
| `StockReserved` | Reservation | `ReserveStock` succeeds | `reservationId`, `sku`, `quantity`, `demandRef` |
| `ReservationExpired` | Reservation | A reservation's timeout elapses before confirmation — **defined, not yet raised**, see below | `reservationId` |
| `ReservationRevoked` | Reservation | `RevokeReservation` succeeds | `reservationId` |
| `StockPicked` | Reservation | `ConfirmPick` consumes a reservation | `reservationId`, `sku`, `quantity` |
| `ItemUnlocated` | StockUnit | A cycle-count shortfall cannot account for stock | `stockUnitId`, `sku`, `binId`, `quantity` |
| `CycleCountCompleted` | Bin | Any cycle count finishes, clean or not | `binId`, `countedQty`, `systemQty`, `discrepancy` |
| `DiscrepancyDetected` | Bin | A cycle count finds counted ≠ system | `binId`, `countedQty`, `systemQty` |
| `ProductClassified` | ProductClassification | `ClassifyProduct` registers or replaces a SKU's classification | `sku`, `handlingTags`, `temperatureClass` |

## Which events flow where

```mermaid
flowchart LR
  RS["ReceiveStock"] --> E1["StockReceived"]
  SS["StowStock"] --> E2["ItemStowed"]
  SS --> E3["LocationRecorded"]
  RES["ReserveStock"] --> E4["StockReserved"]
  REV["RevokeReservation"] --> E5["ReservationRevoked"]
  CP["ConfirmPick"] --> E6["StockPicked"]
  CC["RunCycleCount"] --> E7["CycleCountCompleted"]
  CC --> E8["DiscrepancyDetected"]
  CC --> E9["ItemUnlocated"]
  EXP["timeout"] --> E10["ReservationExpired"]
  CLS["ClassifyProduct"] --> E11["ProductClassified"]

  E1 & E2 & E3 & E6 & E7 & E8 & E9 & E10 & E11 --> LOG["ports.EventPublisher<br/>in-process only"]
  E4 & E5 --> KAF["Kafka<br/>warehouse.inventory.events"]

  classDef wired fill:#0f766e,stroke:#134e4a,color:#fff;
  classDef local fill:#94a3b8,stroke:#475569,color:#0f172a;
  class E4,E5,KAF wired;
  class LOG local;
```

**Only `StockReserved` and `ReservationRevoked` cross the service boundary
today.** The Kafka adapter's `switch` has a `default: return nil` branch that
silently drops everything else — that is deliberate, not an oversight: those
two are the published integration contract, and the other nine (including
`ProductClassified`) are local concerns. `apis/asyncapi.yaml` documents the
full catalog and marks each catalog-only message as such, so a downstream
team cannot mistake a documented event for a wired one.

## One honest gap: nothing sweeps expirations yet

`ReservationExpired` and `Reservation.Expire()` exist in the domain and are
unit-tested, but **no use case calls `Expire()` and nothing publishes
`ReservationExpired` today** — there is no background sweeper. The timeout is
still enforced, just lazily and at a different point:

- `Reservation.Confirm(now)` returns `ErrExpired` past `expiresAt`, so a
  timed-out reservation can never be confirmed into a pick;
- a timed-out reservation's status remains `ACTIVE` in storage, so
  `RevokeReservation` still accepts it and returns its quantity to usable.

The practical consequence is that a reservation nobody revokes keeps holding
quantity out of usable until someone calls `DELETE /reservations/{id}`. A
sweeper that periodically expires and releases them is a real gap, listed here
rather than papered over. `apis/asyncapi.yaml` likewise documents
`ReservationExpired` as catalog-only.

## Naming conventions

Two conventions coexist, and the difference is worth understanding.

### In-process: bare past-tense names

`shared.DomainEvent.EventName()` returns the bare name — `"StockReserved"`,
`"ItemStowed"`. That is the domain's own vocabulary and it deliberately carries
no transport or platform naming.

### On the wire: reverse-DNS CloudEvents `type`

The platform-wide convention, identical across all five services:

```text
com.warehouse.<subdomain>.<bounded-context>.<entity>.<EventName>
```

All lowercase except the final PascalCase event name. For this context the
subdomain segment is `wms` and the bounded context is `inventory-storage`:

```text
com.warehouse.wms.inventory-storage.stock.ItemStowed
com.warehouse.wms.inventory-storage.reservation.ReservationRevoked
com.warehouse.wms.inventory-storage.bin.CycleCountCompleted
```

Entity segments group by the aggregate that raises the event: `stock`,
`reservation`, `bin`.

`StockPicked` is grouped under `reservation` rather than `stock`, because it is
emitted by `ConfirmPick` when a reservation is consumed and the reservation id
is the only identity it carries.

## What the Kafka adapter actually emits today

There is a documented gap between the target envelope and the shipped one, and
it is stated in `apis/asyncapi.yaml` rather than papered over. The adapter
currently writes the **legacy flat warehouse envelope**:

```json
{
  "event_id": "9f1c...-uuid-v4",
  "event_type": "StockReserved",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "inventory-storage",
  "data": { "sku": "SKU-1", "quantity": 6, "demand_ref": "order-42" }
}
```

The `data` payloads for `StockReserved` and `ReservationRevoked` match the
AsyncAPI document field-for-field; the surrounding CloudEvents context
attributes (`specversion`, `id`, `source`, `type`, `subject`, `time`,
`datacontenttype`) describe the target envelope the platform is standardising
on. See the [Events page](/docs/api-reference/events) for both shapes in full,
and [ADR 0004](/docs/adr/0004-kafka-integration-events) for the decision.

## A detail worth knowing: `ReservationRevoked` enrichment

The domain event `ReservationRevoked` carries only a `reservationId` — the
aggregate has no reason to repeat data the reservation already holds. But the
integration contract promises `{sku, quantity, demand_ref}`, because the
downstream projection is keyed by SKU and cannot do a lookup.

The Kafka adapter bridges that gap by re-reading the reservation through
`ReservationRepo` at publish time:

```go
case shared.ReservationRevoked:
	res, err := p.reservations.FindByID(ctx, e.ReservationID)
	if err != nil { return err }
	if res == nil { return ErrReservationNotFound }
	data = reservationData{SKU: res.SKU().String(), Quantity: res.Quantity().Int(), DemandRef: res.DemandRef()}
```

This is the right place for it: enrichment for a downstream consumer's
convenience is an **adapter** concern, and putting the extra fields on the
domain event to save a lookup would let an integration requirement leak into
the domain model. The use case saves the reservation before publishing, so the
lookup always succeeds; `ErrReservationNotFound` exists as a guard, not as an
expected path.

## Read models are projections

`CLAUDE.md` states it as a rule: read models (usable-by-SKU, bin occupancy) are
**projections from events**, not separately-maintained aggregates. Inside this
service, `GetUsable` projects from `StockUnit`s at read time. Across the
boundary, `wes-work-planning` projects `StockReserved` / `ReservationRevoked`
into its own `UsableInventoryObserved` read model keyed by SKU. Same discipline,
two scopes.
