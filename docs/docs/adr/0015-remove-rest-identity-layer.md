---
id: 0015-remove-rest-identity-layer
slug: /adr/0015-remove-rest-identity-layer
title: 15. Remove the fleet REST identity layer (static bearer keys, read/read-write scopes)
sidebar_label: 15. Remove REST identity layer
sidebar_position: 15
description: "inventory-storage removes the static-bearer-key + scope middleware adopted in ADR-0014, from both the REST surface (internal/adapters/inbound/http) and the MCP surface (internal/adapters/inbound/mcp, cmd/mcp), along with the internal/adapters/inbound/auth package, the outbound facility-layout bearer, and the associated Helm chart values/secrets. This ADR supersedes 0014."
---

# 15. Remove the fleet REST identity layer

## Status

**Accepted.** 2026-09-09. Supersedes
[14. Adopt the fleet REST identity standard](./0014-rest-identity-adoption.md).

## Decision

The fleet-wide static-bearer-key auth rollout (ADR-0014, adopting
warehouse-ops-agent ADR 0005) is removed from this repository. The `Scope` /
`Authenticator` / `StaticKeyAuth` / `Middleware` types and the
`internal/adapters/inbound/auth` package they lived in are deleted
entirely. Both inbound surfaces this service exposes are unauthenticated
at the application layer again:

- **REST** (`internal/adapters/inbound/http`): `NewRouter` and
  `NewReportsRouter` no longer accept or apply an auth middleware. Every
  route (including the ones that used to require the read-write scope) is
  mounted directly on the chi router with no gating group. The
  `RouterOption`/`routerConfig` extension point is kept as an empty shape
  rather than removed outright, so the function signatures composition
  roots call do not need to change again if a different cross-cutting
  concern needs the same seam later.
- **MCP** (`internal/adapters/inbound/mcp`, `cmd/mcp`): `Handler` no longer
  takes an `Authenticator` and wraps the Streamable HTTP handler directly;
  tools, resources, and prompts no longer take or check a `scopeOf`
  callback. `cmd/mcp` no longer reads `MCP_READ_KEY` / `MCP_READWRITE_KEY`
  or constructs `StaticKeyAuth`.
- **Composition roots** (`cmd/inventory`, `cmd/inventory-reports`,
  `cmd/mcp`): no longer parse `AUTH_MODE`, construct `auth.Middleware`, or
  read `API_READ_KEY` / `API_READWRITE_KEY` / `MCP_READ_KEY` /
  `MCP_READWRITE_KEY`.
- **Outbound facility-layout client**
  (`internal/adapters/outbound/facilitylayout`): `WithBearer` and
  `FACILITY_LAYOUT_API_KEY` are removed; the client sends no
  `Authorization` header.
- **Chart** (`charts/inventory-storage`): the `auth` values block,
  `facilityLayout.apiKey`, `mcp.readKey`/`mcp.readWriteKey`/
  `mcp.existingSecret` are removed from `values.yaml`; `AUTH_MODE` is
  removed from the ConfigMap; `API_READ_KEY`/`API_READWRITE_KEY`/
  `FACILITY_LAYOUT_API_KEY` are removed from the auth Secret (the whole
  conditional auth Secret block in `templates/secret.yaml` is gone, along
  with the now-dead `authSecretName`/`authEnabled`/`authEnv`/
  `mcpSecretName` helpers and `templates/mcp-secret.yaml`).
- **Docs**: `apis/openapi.yaml` drops `components.securitySchemes.bearerAuth`
  and every `security:` key (including the `/healthz` `security: []`
  override, which is moot once there is no default security requirement to
  override). `apis/asyncapi.yaml` carried no security scheme to begin with.
  The README's auth env-var table row and the MCP-in-Kubernetes section's
  bearer-key instructions are removed.

## Consequences

- Both REST and MCP surfaces are open at the application layer again,
  exactly as they were before ADR-0014. Any access control this platform
  wants going forward is expected to live at the network/mesh layer or in
  a future, better-scoped decision — not resurrected as a copy of the
  static-bearer scheme this ADR removes.
- The OAuth 2.1 resource-server seam ADR-0008 originally described (and
  ADR-0014 preserved through the `Authenticator` interface) is gone along
  with the interface itself. A future identity decision starts from a
  clean slate rather than extending the removed seam.
- `CLAUDE.md` documents the REST API list under a `## REST API` heading
  with no auth callouts; it was not modified as part of this change
  (CLAUDE.md is protected in this environment) — its content did not
  describe per-route auth requirements, so no correction was needed there.
- ADR-0014 is left in place, unmodified, and marked superseded by this
  ADR — it remains the historical record of what was adopted and why.
