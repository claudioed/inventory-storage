---
title: Bounded-Context Relationships
sidebar_label: Context Relationships
description: How this bounded context relates to the other four, in Evans/Vernon context-mapping vocabulary.
---

# Bounded-Context Relationships

This page is the *strategic* view — the DDD relationship patterns. The
*technical* view (topics, envelopes, payloads) is on the
[Ecosystem](/docs/ecosystem/context-map) pages.

## Pattern vocabulary

Using the patterns as `warehouse-systems-ddd.md` and
`amazon-fulfillment-ddd.md` use them:

| Pattern | Meaning |
| --- | --- |
| **OHS** — Open Host Service | A context exposes a well-defined, public protocol for many consumers, rather than a bespoke integration per consumer. |
| **PL** — Published Language | The shared, documented vocabulary that protocol speaks (here: the REST DTOs + the event catalog). |
| **C/S** — Customer/Supplier | Upstream supplies something downstream needs; downstream's needs have standing with upstream. |
| **CF** — Conformist | Downstream accepts upstream's model as-is, with no translation leverage. |
| **ACL** — Anti-Corruption Layer | Downstream translates upstream's model into its own, so upstream concepts never leak inward. |

## This context's role: **Open Host Service, upstream**

`inventory-storage` is an **Open Host Service** for two things:

1. **bin-accurate location** — where stock physically is, and
2. **usable inventory** — what portion of it can be promised.

Its **Published Language** has two surfaces:

- **REST** (`apis/openapi.yaml`) — synchronous queries and commands.
- **Events** (`apis/asyncapi.yaml`, topic `warehouse.inventory.events`) —
  asynchronous facts.

Both are versioned, Spectral-linted artefacts in the repository, not
implementation details. That is what makes it an *Open Host* Service rather
than a set of point-to-point integrations: a sixth consumer could arrive
tomorrow and integrate from the published spec without this repo changing.

The reference model states the position directly:

> **Planning ↔ Inventory & Storage:** Customer/Supplier. Planning asks "can I
> allocate?"; Inventory is the authoritative supplier of stock + bin location
> (its Open-Host Service exposes bin-accurate location as Published Language).

## The map

```mermaid
flowchart TB
    INV["<b>inventory-storage</b><br/>WMS · Core<br/><i>OHS + Published Language</i>"]
    WP["<b>wes-work-planning</b><br/>WES · Core<br/>the conductor"]
    FE["<b>fulfillment-execution</b><br/>WES · Core<br/>Pick/Pack/SLAM"]
    WM["<b>workforce-management</b><br/>Supporting<br/>headcount + assignment"]
    FL["<b>facility-layout</b><br/>Generic<br/>physical warehouse map"]

    INV -->|"U → D · C/S<br/>Conformist to PL<br/><b>wired: Kafka</b>"| WP
    WM -->|"U → D · C/S<br/><b>wired: Kafka</b>"| WP
    WP -->|"U → D · C/S<br/><b>wired: Kafka</b>"| FE
    FE -->|"U → D<br/><b>wired: Kafka</b>"| WP
    FL -.->|"OHS + PL<br/><b>not wired</b> — strategic only"| INV
    FL -.->|"not wired"| WP

    classDef this fill:#0f766e,stroke:#134e4a,color:#fff,stroke-width:3px;
    classDef other fill:#1e293b,stroke:#475569,color:#fff;
    classDef future fill:#475569,stroke:#94a3b8,color:#e2e8f0,stroke-dasharray: 5 5;
    class INV this;
    class WP,FE,WM other;
    class FL future;
```

Solid edges are actually implemented today. Dashed edges are strategic
relationships with **no code behind them yet**.

## Relationship by relationship

### inventory-storage → wes-work-planning — **Customer/Supplier, Conformist downstream**

The one live integration this service participates in.

- **Direction:** upstream (supplier). This service publishes; Work Planning
  consumes.
- **Pattern:** Customer/Supplier with Work Planning as a **Conformist** to this
  service's Published Language. It takes `StockReserved` /
  `ReservationRevoked` in the shape they are published and projects them into
  its own `UsableInventoryObserved` read model keyed by SKU — it does not
  negotiate the shape, and it does not get write access.
- **Why no write access:** `warehouse-systems-ddd.md` is explicit — "WES does
  **not** get write access to WMS's Order/Inventory aggregates — only to the
  published events/API." Reserving stock is a `POST /reservations` call against
  this service's own API, which runs this service's own invariants. There is no
  path by which a WES-tier service can mutate a `StockUnit` directly.
- **The ACL runs in both directions.** This context never learns what a "work
  unit," a "CPT," a "process path" or a "station" is. Work Planning never
  learns what a `StockUnit`, a `Bin` or an `Allocation` is — it holds only an
  observed usable count per SKU. That mutual ignorance is the boundary working.

### inventory-storage ↔ fulfillment-execution — **indirect, via Work Planning**

There is **no direct wiring** between these two. `fulfillment-execution`
consumes `warehouse.work-planning.events` (`WorkReleased`) and publishes
`warehouse.fulfillment.events`; it does not subscribe to
`warehouse.inventory.events`, and this service does not subscribe to anything.

Strategically that is right: `fulfillment-execution` owns the *task* lifecycle
and needs work to do, not stock truth. The accounting consequence of a pick
reaches this service as an explicit `POST /reservations/{id}/confirm-pick`
call, which is a deliberate command, not an event this service happens to
overhear.

### inventory-storage ↔ workforce-management — **no relationship**

Labour planning and inventory truth share no concepts. `workforce-management`
publishes `ShiftPlanCommitted` on `warehouse.workforce.events`, consumed by
Work Planning only. Keeping these two contexts unrelated is the concrete form
of the rule that worker identity must not leak into the system of record.

### inventory-storage ← facility-layout — **strategically Conformist, technically not wired**

`facility-layout` is a **Generic subdomain** and an **Open Host Service** for
physical warehouse structure: `Site → Area → Zone → Aisle → Bay → Level →
Position`, `LocationType`, `PlacementRule`, `LocationSlot`. Its own `CLAUDE.md`
positions the other four services — this one included — as downstream
**Conformists** to whatever it publishes.

**Today there is nothing to conform to in code.** This repository has no
consumer, no dependency and no configuration referencing `facility-layout`, and
`facility-layout` has no Kafka adapter at all — only an in-process log
publisher. Wiring it up is described in that repo as "a separate, later,
additive task in those repos — out of scope here."

The intended shape, when it is built:

- `facility-layout` becomes the source of truth for whether a `BinId` is a real,
  active, correctly-typed slot;
- this service's `StowStock` validates the location scan against it, instead of
  the current "the bin must exist in `LocationRepo`" check;
- placement policy (temperature class, hazmat, size fit) stays in
  `facility-layout`'s `PlacementRule` model, and never migrates here — that
  would be re-introducing fixed slotting through the back door.

Until then, a `Bin` in this context remains an id, a capacity and an occupancy,
seeded as infrastructure data.

## Disciplines this map enforces

From `warehouse-systems-ddd.md`, applied literally in this repository:

1. **No shared aggregates across contexts.** No Go type in this module is
   imported by any sibling; no sibling type is imported here. Integration is
   JSON over Kafka and HTTP.
2. **Same word, different model is allowed.** "Location," "Reservation" and
   "Pick" all mean different things one service over — see the
   [Ubiquitous Language](/docs/business-context/ubiquitous-language) table.
3. **Model synchronization explicitly.** Fan-in across concurrent work
   (`OrderConvergence`) is a WES concern and is *absent* here by design; this
   context has no cross-order coordination concept at all.
4. **Classify strategically before modelling tactically.** Core classification
   is what justifies the domain-purity rules and the quality bar; it is also
   what justifies *not* building a facility-layout model here just because it
   would be convenient.
