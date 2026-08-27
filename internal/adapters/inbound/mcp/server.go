package mcp

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// scopeKey is the context key under which the authenticated scope is carried
// from the auth middleware into tool/resource handlers.
type scopeKey struct{}

// scopeFromContext returns the scope stored by the auth middleware, or the
// empty scope if none is present (which scopeAllows treats as unauthorized).
func scopeFromContext(ctx context.Context) Scope {
	if s, ok := ctx.Value(scopeKey{}).(Scope); ok {
		return s
	}
	return ""
}

// NewServer builds the MCP server for this bounded context with every read
// tool, scoped resource, and workflow prompt registered. Handlers read the
// authenticated scope from their context (placed there by Handler's
// middleware).
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "inventory-storage-mcp", Version: "1.0.0"},
		&mcp.ServerOptions{
			Instructions: "Access to the inventory-storage context: usable availability by SKU, bin occupancy, and revocable reservation release. Usable — not total on-hand — is what constrains a release. Start with the triage_low_stock prompt.",
		},
	)

	deps.registerTools(server, scopeFromContext)
	deps.registerResources(server, scopeFromContext)
	deps.registerPrompts(server, scopeFromContext)

	return server
}

// Handler returns the Streamable HTTP handler for the MCP server, wrapped in
// the auth middleware. Every request must carry a valid bearer key; the scope
// it grants is placed in the request context for handlers to enforce per-tool.
//
// This is the single seam described in ADR-0008: replacing StaticKeyAuth with
// an OAuth 2.1 resource-server Authenticator changes only what is passed here,
// not any handler.
func Handler(server *mcp.Server, auth Authenticator) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := auth.Authenticate(r)
		if !ok {
			// Signal how to authenticate without leaking any detail about why
			// the credential failed.
			w.Header().Set("WWW-Authenticate", `Bearer realm="inventory-storage-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), scopeKey{}, scope)
		streamable.ServeHTTP(w, r.WithContext(ctx))
	})
}
