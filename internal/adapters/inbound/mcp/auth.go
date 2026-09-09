package mcp

import "github.com/claudioed/inventory-storage/internal/adapters/inbound/auth"

// The MCP adapter's identity model is the fleet-standard one in
// internal/adapters/inbound/auth (ADR-0014, adopting warehouse-ops-agent
// ADR 0005): static bearer keys mapped to read / read-write scopes behind an
// Authenticator seam. This adapter used to carry its own copy of those types
// (ADR-0008); they now live in the shared package so one repository has
// exactly one implementation serving both the REST and MCP surfaces. The
// aliases below keep this package's public surface (and cmd/mcp) unchanged.

// Scope is a coarse authorization class carried by an API key.
type Scope = auth.Scope

const (
	ScopeRead      = auth.ScopeRead
	ScopeReadWrite = auth.ScopeReadWrite
)

// Authenticator validates a request's bearer credential and reports the scope
// it grants — the ADR-0008 seam for a future OAuth 2.1 resource server.
type Authenticator = auth.Authenticator

// StaticKeyAuth authenticates against a fixed set of bearer keys.
type StaticKeyAuth = auth.StaticKeyAuth

// NewStaticKeyAuth builds a StaticKeyAuth from token->scope pairs.
func NewStaticKeyAuth(keys map[string]Scope) *StaticKeyAuth {
	return auth.NewStaticKeyAuth(keys)
}

// scopeAllows reports whether a granted scope may call a tool requiring the
// given minimum scope. read-write satisfies everything; read satisfies only
// read.
func scopeAllows(granted, required Scope) bool {
	return auth.Allows(granted, required)
}
