package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds the MCP server for this bounded context with every read
// tool, scoped resource, and workflow prompt registered.
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "inventory-storage-mcp", Version: "1.0.0"},
		&mcp.ServerOptions{
			Instructions: "Access to the inventory-storage context: usable availability by SKU, bin occupancy, and revocable reservation release. Usable — not total on-hand — is what constrains a release. Start with the triage_low_stock prompt.",
		},
	)

	deps.registerTools(server)
	deps.registerResources(server)
	deps.registerPrompts(server)

	return server
}

// Handler returns the Streamable HTTP handler for the MCP server. The fleet
// static-bearer auth layer that used to gate this handler (ADR-0008) has
// been removed; the server is mounted unauthenticated.
func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}
