package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	inboundmcp "github.com/claudioed/inventory-storage/internal/adapters/inbound/mcp"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

// newTestRouter builds the exact HTTP surface cmd/mcp serves, with the real
// inbound MCP adapter and static-key auth, over in-memory adapters.
func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	stock := memory.NewStockRepo()
	deps := inboundmcp.Deps{
		GetUsable: &usecases.GetUsable{Stock: stock},
		Stock:     stock,
	}
	auth := inboundmcp.NewStaticKeyAuth(map[string]inboundmcp.Scope{"read-key": inboundmcp.ScopeRead})
	return newRouter(inboundmcp.Handler(inboundmcp.NewServer(deps), auth))
}

func TestHealthzIsUnauthenticated(t *testing.T) {
	router := newTestRouter(t)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz without a bearer key: got %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"status":"ok"}` {
		t.Errorf("body = %q, want {\"status\":\"ok\"}", body)
	}
}

// Only GET /healthz is open: any other method on that path falls through to
// the catch-all MCP handler and therefore to the bearer check (fail closed).
func TestHealthzOnlyOpenForGET(t *testing.T) {
	router := newTestRouter(t)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /healthz without a bearer key: got %d, want 401", rec.Code)
	}
}

func TestMCPRoutesRequireBearer(t *testing.T) {
	router := newTestRouter(t)

	for _, path := range []string{"/", "/mcp", "/mcp/"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("POST %s without a bearer key: got %d, want 401", path, rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
		})
	}
}

func TestMCPRoutesReachAuthenticatedHandler(t *testing.T) {
	router := newTestRouter(t)

	for _, path := range []string{"/", "/mcp"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Authorization", "Bearer read-key")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// With a valid key the request is past auth and inside the MCP
			// Streamable HTTP handler: whatever the SDK answers, it must not
			// be the auth middleware's 401 and it must not be the mux's 404.
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
				t.Fatalf("POST %s with a valid key: got %d, expected the MCP handler to be reached", path, rec.Code)
			}
		})
	}
}
