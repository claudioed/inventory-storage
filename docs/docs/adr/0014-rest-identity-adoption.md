---
id: 0014-rest-identity-adoption
slug: /adr/0014-rest-identity-adoption
title: 14. Adopt the fleet REST identity standard (static bearer keys, read/read-write scopes)
sidebar_label: 14. REST identity adoption
sidebar_position: 14
description: "inventory-storage adopts warehouse-ops-agent ADR 0005: every REST route except /healthz is protected by the same static bearer key + scope middleware the MCP adapter already carried, lifted into internal/adapters/inbound/auth so both surfaces share one implementation, rolled out through AUTH_MODE=log before enforce."
---

# 14. Adopt the fleet REST identity standard

## Status

**Superseded** by
[15. Remove the fleet REST identity layer](./0015-remove-rest-identity-layer.md)
(2026-09-09). Originally accepted 2026-09-07; the decision itself was
[warehouse-ops-agent ADR 0005 — Fleet REST identity: static bearer keys
with read/read-write scopes, no IdP](https://github.com/claudioed/warehouse-ops-agent/blob/develop/docs/docs/adr/0005-rest-identity-static-bearer-scopes.md).

## Decision

inventory-storage applies ADR 0005 as written. The `Scope` / `Authenticator` /
`StaticKeyAuth` seam that ADR-0008 gave the MCP adapter moves to
`internal/adapters/inbound/auth` (the fleet template, copied verbatim) and
gains a chi `Middleware`; the MCP adapter now imports it instead of keeping
its own copy, so this repository has exactly one identity implementation
for both surfaces. `NewRouter` mounts the middleware on every route group
through a chi `Group`, leaving `/healthz` outside it; `NewReportsRouter`
mounts the same middleware with the required scope pinned to **read**,
because the reader is read-only by construction (ADR-0011). Safe methods
need the read scope, everything else read-write; rejections are RFC 7807
problems under this service's existing type base (`…/unauthenticated`,
`…/insufficient-scope`) with a `WWW-Authenticate: Bearer` challenge on
401. Keys come from `API_READ_KEY` / `API_READWRITE_KEY` (falling back to
`MCP_READ_KEY` / `MCP_READWRITE_KEY` so one Secret can serve both
deployables); `AUTH_MODE` selects `enforce` | `log` | `off`, defaulting to
`enforce` when a key is configured and `off` — with a WARN — when none is,
so local runs and the existing handler tests are unchanged. The chart
exposes `auth.mode` / `auth.readKey` / `auth.readWriteKey` /
`auth.existingSecret`, renders `AUTH_MODE` into the ConfigMap and the key
refs into both the main and the reports Deployment; the outbound
facility-layout client (used only when `LOCATION_LOOKUP_MODE=http`) sends
`FACILITY_LAYOUT_API_KEY` as its own bearer. warehouse-infra rolls
this out cluster-wide in two phases (`log`, soak for `auth: would-reject`,
then `enforce`), exactly as ADR 0005 prescribes. The OAuth 2.1 seam ADR-0008
promised is preserved: swapping `StaticKeyAuth` for a resource-server
`Authenticator` changes only the composition roots.
