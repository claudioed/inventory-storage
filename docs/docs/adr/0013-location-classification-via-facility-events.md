---
id: 0013-location-classification-via-facility-events
slug: /adr/0013-location-classification-via-facility-events
title: 13. Location classification from facility-layout's events, not a synchronous call
sidebar_label: 13. Location classification via events
sidebar_position: 13
description: ADR 0013 — replace the per-stow synchronous HTTP read from facility-layout with a locally-maintained read model fed by that context's Published Language, gated on a full replay at startup.
---

# 13. Location classification from facility-layout's events, not a synchronous call

## Status

Accepted.

## Context

ADR 0009 introduced hazmat/temperature placement rules at stow time, and
wired them to `facility-layout` through a synchronous HTTP read:
`StowStock` calls `GET /locations/{locationCode}/classification` on every
stow of a classified SKU. That was the right first step — it got the rule
enforced against the real owner of the data without duplicating it — but it
has three properties that get worse as the estate grows:

1. **`facility-layout` became a runtime dependency of stowing.** If it is
   down, redeploying, or slow, stows of classified SKUs fail or stall for
   `DefaultTimeout` (5s). A warehouse's ability to put stock away should not
   hinge on the availability of a *reference data* service.
2. **The read rate and the change rate are wildly mismatched.** Zone
   attributes change on the order of a handful of times a week; the stow
   path reads them continuously. We were paying a network round trip per
   stow to re-fetch data that had almost certainly not changed.
3. **It is a synchronous, runtime coupling between bounded contexts** in an
   estate that otherwise integrates through Kafka. `facility-layout`
   already publishes its Published Language to
   `warehouse.facility.events` (its ADR 0009); we were simply not
   consuming it.

The data in question is a textbook fit for a locally-maintained read model:
small, slow-changing, read-hot reference data with a single clear owner.

## Decision

Add a third `LOCATION_LOOKUP_MODE`, `kafka`, backed by a new outbound
adapter (`internal/adapters/outbound/facilitycache`) that maintains an
in-memory view of location classifications by consuming
`warehouse.facility.events`, and satisfies the existing
`ports.LocationClassificationLookup` port.

**No use case, domain type, or call site changes.** The port already
existed (ADR 0009 deliberately introduced it rather than letting
`StowStock` know about HTTP), so this is a pure adapter swap at the
composition root.

This makes `inventory-storage` a **Conformist** to `facility-layout`'s
Published Language: that context remains the sole source of truth for zone
attributes, and this one only mirrors them, never writes them.

Three design points are load-bearing:

**Zones and slots are kept in separate maps and joined at lookup time.** A
slot and its zone are different aggregates, published under different
partition keys, so there is *no* cross-aggregate ordering guarantee — a
`LocationSlotRegistered` can legitimately be replayed before the
`ZoneRegistered` it refers to. Joining lazily means such a slot resolves
correctly once its zone arrives, instead of being permanently cached with
missing attributes.

**The consumer group id is unique per process instance**
(`hostname+PID+nanos`). Kafka consumer group offsets are shared
infrastructure state, not per-process state. This consumer's job is to
rebuild a *complete* cache from the topic's full history on every start —
an event-sourced read model, not a work queue. A fixed group name would
make a restarted process resume from the previous instance's committed
offset and come up "ready" with an empty cache, having replayed nothing.

**Readiness is gated on a full replay of what existed at startup.** An
empty cache is indistinguishable from "this location is not modeled in
facility-layout", which `GetSlotAttributes` reports as `Known=false` — a
**fail-open** answer. Serving traffic mid-replay would therefore silently
wave through stows that should have been classified. The consumer captures
each partition's last offset before consuming and only reports ready once
it has itself processed up to that watermark. This preserves the intent of
the HTTP client's guarantee (never answer against incomplete data) with a
mechanism appropriate to an event-sourced cache.

## Consequences

### Positive

- `facility-layout` is no longer a runtime dependency of `StowStock`. It
  can be down entirely and stows keep being classified correctly from the
  cache.
- The per-stow network round trip disappears; lookups become a map read.
- `ErrLocationClassificationUnavailable` is unreachable through this
  adapter — it cannot fail, because it performs no I/O.
- Layout changes propagate to running pods within seconds, with no restart
  and no redeploy.

### Negative / accepted trade-offs

- **Eventual consistency.** There is a window (seconds) between
  `facility-layout` publishing a change and this cache reflecting it. A
  stow into a location registered moments ago may briefly see
  `Known=false` and be waved through fail-open. This is acceptable: the
  same fail-open answer is what the HTTP path returned for a 404, and
  newly-registered locations are not in active stow rotation in that
  window.
- **Correctness depends on the topic carrying the full history.** A fresh
  consumer replays from the earliest offset, so the topic must not be
  aggressively retained/compacted away, and `facility-layout` must
  actually be publishing (`EVENT_PUBLISHER=kafka` — its default is the
  Postgres outbox). If the topic is empty the service starts, logs a loud
  warning, and fails open on every lookup.
- **Memory.** The whole classification view is held in memory. At warehouse
  scale (thousands of slots, tens of zones) this is trivially small; a
  facility orders of magnitude larger would want a different structure.
- This context now consumes another's events, so `facility-layout`'s
  Published Language has a real downstream consumer: changes to those
  event shapes are now breaking changes for us.

### Rollback

Pure configuration: set `LOCATION_LOOKUP_MODE=http` (with
`FACILITY_LAYOUT_BASE_URL`) and restart. The HTTP client is deliberately
retained, not deleted, for exactly this reason. `permissive` remains the
default, so deployments that set nothing are unaffected.

## Alternatives considered

**Keep the synchronous call, add a TTL cache in front of it.** Simpler, but
keeps `facility-layout` as a runtime dependency on every cache miss and on
every TTL expiry, and introduces a staleness window that is *less*
predictable than the event-driven one (a TTL cache can serve data
arbitrarily stale up to the TTL even when a change was published
immediately). Rejected.

**Cache with HTTP fallback on a miss.** Tempting, but it reintroduces
precisely the runtime coupling this ADR removes, and does so on the
unhappy path where `facility-layout` is most likely to be unavailable. It
also makes a genuine "not modeled" answer indistinguishable from a cache
gap. Deferred — if the eventual-consistency window ever proves to be a
real operational problem, this is the escape hatch, but it should not be
built preemptively.

**Replicate the data into this context's own Postgres.** Would survive
restarts without a replay, but adds a schema, migrations, and a projector
process to maintain — significant machinery for data small enough to hold
in memory and cheap enough to replay in seconds. Rejected as YAGNI.

## Related

- ADR 0009 — product classification and the placement rules this lookup
  serves; introduced the port that made this swap a one-line change.
- ADR 0004 — Kafka integration events; the estate convention this follows.
- `facility-layout` ADR 0009 — the Published Language being consumed here.
