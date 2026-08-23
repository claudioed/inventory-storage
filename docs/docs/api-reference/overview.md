---
slug: /api-reference
title: API Reference
sidebar_label: Overview
description: REST conventions, the full endpoint matrix, and the RFC 7807 error catalog.
---

# API Reference

This service exposes two contracts, both kept as source-of-truth artefacts in
the repository and both linted by Spectral in CI:

| Contract | File | Rendered here |
| --- | --- | --- |
| REST (synchronous) | `apis/openapi.yaml` — OpenAPI 3.0.3 | **[REST API](./rest/inventory-storage-api.info.mdx)** — generated directly from the spec |
| Events (asynchronous) | `apis/asyncapi.yaml` — AsyncAPI 2.6.0 | **[Events](./events.md)** |

The REST pages under **REST API** are *generated* from `apis/openapi.yaml` by
`docusaurus-plugin-openapi-docs` at build time. They are not hand-transcribed,
so they cannot drift from the spec the service ships.

## Endpoint matrix

All 8 routes registered in `internal/adapters/inbound/http/server.go` are
documented — **8 / 8**.

| Method | Path | Operation | Tag | Success | Errors |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/healthz` | `getHealthz` | Health | `200` | — |
| `POST` | `/stock/receive` | `receiveStock` | Stock | `202` | `400` `422` `500` |
| `POST` | `/stock/stow` | `stowStock` | Stock | `201` | `400` `404` `409` `422` `500` |
| `POST` | `/reservations` | `reserveStock` | Reservations | `201` | `400` `409` `422` `500` |
| `DELETE` | `/reservations/{id}` | `revokeReservation` | Reservations | `204` | `404` `409` `500` |
| `POST` | `/reservations/{id}/confirm-pick` | `confirmPick` | Reservations | `204` | `404` `409` `500` |
| `GET` | `/inventory/{sku}/usable` | `getUsableInventory` | Inventory | `200` | `400` `500` |
| `POST` | `/bins/{binId}/cycle-count` | `runCycleCount` | Bins | `200` | `400` `404` `422` `500` |

## Status-code conventions

The status codes are deliberate, not defaults. The reasoning was recorded
during the REST hardening pass:

| Code | Used for | Example |
| --- | --- | --- |
| `200 OK` | A read, or a command whose result *is* the response body | `GET /inventory/{sku}/usable`; `POST /bins/{binId}/cycle-count` returns the reconciliation result |
| `201 Created` | A new addressable resource, with a `Location` header | `POST /stock/stow` → `Location: /stock/{id}`; `POST /reservations` → `Location: /reservations/{id}` |
| `202 Accepted` | Accepted, but no addressable resource exists yet | `POST /stock/receive` — a staged receipt has no id and no `GET` route; the `StockUnit` is created later, at stow |
| `204 No Content` | A state transition with nothing useful to return | revoke, confirm-pick |
| `400 Bad Request` | Malformed or missing input | empty SKU, empty bin id, unparseable JSON |
| `404 Not Found` | The addressed resource does not exist | unknown reservation, unknown bin |
| `409 Conflict` | Well-formed and addressable, but conflicts with current state | bin full, reservation exceeds usable, reservation already resolved, reservation expired |
| `422 Unprocessable Entity` | Well-formed but semantically invalid *values* | quantity ≤ 0, negative quantity, non-positive bin capacity |

The `400` / `422` split is the one worth internalising: `400` means "I could
not understand the request," `422` means "I understood it perfectly and it is
not a legal thing to ask for."

## Errors: RFC 7807 Problem Details

Every error response uses `application/problem+json`:

```json
{
  "type": "https://errors.inventory-storage.warehouse-systems.dev/insufficient-usable",
  "title": "Requested quantity exceeds usable inventory",
  "status": 409,
  "detail": "requested quantity exceeds usable inventory",
  "instance": "/reservations"
}
```

- `type` is a stable, unique URI per error **category**. It is an identifier —
  it does not have to resolve to a page.
- `title` is a fixed human string for the category; safe to switch on for
  display, though `type` is the machine-readable key.
- `detail` is the dynamic message from the underlying typed error.
- `instance` is the request path.

### Problem-type catalog

Mapping is one-for-one with the typed domain and application errors, in
`internal/adapters/inbound/http/errors.go`.

| `type` slug | Status | Raised by |
| --- | --- | --- |
| `stock-unit-not-found` | 404 | `usecases.ErrStockUnitNotFound` |
| `bin-not-found` | 404 | `usecases.ErrBinNotFound` |
| `reservation-not-found` | 404 | `usecases.ErrReservationNotFound` |
| `empty-sku` | 400 | `shared.ErrEmptySKU` |
| `empty-bin-id` | 400 | `shared.ErrEmptyBinID` |
| `stow-requires-item-and-location` | 400 | `stock.ErrStowRequiresItemAndLocation` |
| `negative-quantity` | 422 | `shared.ErrNegativeQuantity` |
| `zero-quantity` | 422 | `shared.ErrZeroQuantity` |
| `invalid-bin-capacity` | 422 | `location.ErrInvalidCapacity` |
| `bin-full` | 409 | `location.ErrBinFull` |
| `release-exceeds-occupancy` | 409 | `location.ErrReleaseExceedsOccupancy` |
| `insufficient-usable` | 409 | `usecases.ErrInsufficientUsable`, `stock.ErrInsufficientUsable` |
| `insufficient-reserved` | 409 | `stock.ErrInsufficientReserved` |
| `unit-unlocated` | 409 | `stock.ErrUnitUnlocated` |
| `reservation-already-resolved` | 409 | `reservation.ErrAlreadyResolved` |
| `reservation-expired` | 409 | `reservation.ErrExpired` |
| `reservation-no-allocations` | 409 | `reservation.ErrNoAllocations` |
| `internal-error` | 500 | anything unmapped |

The domain never knows about any of this. It returns typed errors; the inbound
adapter is the only layer that translates them. See
[ADR 0005](/docs/adr/0005-rfc-7807-problem-details).

## DTOs never leak domain types

Request and response bodies are adapter-local structs in
`internal/adapters/inbound/http/dto.go`. A `stockUnitResponse` is not a
`stock.StockUnit`; a `reservationResponse` is not a `reservation.Reservation`.
That indirection is what lets the domain model evolve without breaking the wire
contract, and it is enforced by the arch-go fitness tests.

## Authentication

None. `security: []` in the spec is deliberate and explicit: this is an
internal, cluster-local service reached through the platform's gateway
(Kong, north-south) and mesh (Istio), which own authentication and
authorisation. Declaring `security: []` states "no auth at this layer" rather
than leaving it ambiguous.
