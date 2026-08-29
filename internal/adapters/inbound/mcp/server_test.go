package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/claudioed/inventory-storage/internal/adapters/inbound/mcp"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

const readKey = "test-read-key"
const writeKey = "test-write-key"

// bearerTransport adds a fixed Authorization header to every request, so the
// in-process MCP client authenticates like a real one.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

// seed builds in-memory repos with one stowed StockUnit (SKU-A@BIN-1, qty 10)
// and one reservation of qty 4 against it, and returns the repos, the reserve
// use case, and the reservation id. The reservation is what the write-tool
// tests revoke.
func seed(t *testing.T) (*memory.StockRepo, *memory.ReservationRepo, *events.BufferedPublisher, *memory.FixedClock, string) {
	t.Helper()
	stockRepo := memory.NewStockRepo()
	reservationRepo := memory.NewReservationRepo()
	publisher := events.NewBufferedPublisher()
	clock := memory.NewFixedClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	q10, _ := shared.NewPositiveQuantity(10)
	unit, err := stock.NewStockUnit("su-1", shared.SKU("SKU-A"), shared.BinId("BIN-1"), q10)
	if err != nil {
		t.Fatalf("new stock unit: %v", err)
	}
	if err := stockRepo.Save(context.Background(), unit); err != nil {
		t.Fatalf("save unit: %v", err)
	}
	reserve := &usecases.ReserveStock{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock}
	q4, _ := shared.NewPositiveQuantity(4)
	res, err := reserve.Execute(context.Background(), shared.SKU("SKU-A"), q4, "demand-1")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	return stockRepo, reservationRepo, publisher, clock, res.ID()
}

// newServer builds a real MCP HTTP server over the seeded repos with a
// read-only key, and returns its httptest URL.
func newServer(t *testing.T) string {
	t.Helper()
	stockRepo, _, _, _, _ := seed(t)
	deps := inboundmcp.Deps{
		GetUsable: &usecases.GetUsable{Stock: stockRepo},
		Stock:     stockRepo,
	}
	server := inboundmcp.NewServer(deps)
	auth := inboundmcp.NewStaticKeyAuth(map[string]inboundmcp.Scope{readKey: inboundmcp.ScopeRead})
	httpSrv := httptest.NewServer(inboundmcp.Handler(server, auth))
	t.Cleanup(httpSrv.Close)
	return httpSrv.URL
}

// newWriteServer builds a server with both a read and a read-write key over
// the seeded repos, and returns the URL plus the revocable reservation's id.
func newWriteServer(t *testing.T) (string, string) {
	t.Helper()
	stockRepo, reservationRepo, publisher, clock, resID := seed(t)
	deps := inboundmcp.Deps{
		GetUsable:         &usecases.GetUsable{Stock: stockRepo},
		RevokeReservation: &usecases.RevokeReservation{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		Stock:             stockRepo,
	}
	server := inboundmcp.NewServer(deps)
	auth := inboundmcp.NewStaticKeyAuth(map[string]inboundmcp.Scope{
		readKey:  inboundmcp.ScopeRead,
		writeKey: inboundmcp.ScopeReadWrite,
	})
	httpSrv := httptest.NewServer(inboundmcp.Handler(server, auth))
	t.Cleanup(httpSrv.Close)
	return httpSrv.URL, resID
}

func connect(t *testing.T, url, token string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &sdk.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestServer_UnauthenticatedIsRejected(t *testing.T) {
	url := newServer(t)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got == "" {
		t.Fatal("missing WWW-Authenticate challenge on 401")
	}
}

func TestServer_ToolsListAndCall(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	ctx := context.Background()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := map[string]bool{"check_availability": false, "get_bin_occupancy": false, "revoke_reservation": false}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not advertised", name)
		}
	}

	res, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "check_availability",
		Arguments: map[string]any{"sku": "SKU-A"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %+v", res.Content)
	}
	usable, ok := res.StructuredContent.(map[string]any)["usable"]
	if !ok {
		t.Fatalf("no usable in structured content: %+v", res.StructuredContent)
	}
	// 10 on-hand minus 4 reserved = 6 usable.
	if usable.(float64) != 6 {
		t.Fatalf("SKU-A usable = %v, want 6", usable)
	}
}

func TestServer_CallToolRejectsEmptySKU(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "check_availability",
		Arguments: map[string]any{"sku": ""},
	})
	if err != nil {
		t.Fatalf("call tool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool-level error for empty sku")
	}
}

func TestServer_ResourceRead(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	res, err := session.ReadResource(context.Background(), &sdk.ReadResourceParams{
		URI: "inventory://SKU-A/usable",
	})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(res.Contents) == 0 || res.Contents[0].Text == "" {
		t.Fatalf("empty resource contents: %+v", res.Contents)
	}
}

func TestServer_PromptGet(t *testing.T) {
	url := newServer(t)
	session := connect(t, url, readKey)
	res, err := session.GetPrompt(context.Background(), &sdk.GetPromptParams{Name: "triage_low_stock"})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) == 0 {
		t.Fatal("triage_low_stock prompt returned no messages")
	}
}

func TestServer_RevokeReservationDeniedForReadOnlyKey(t *testing.T) {
	url, resID := newWriteServer(t)
	session := connect(t, url, readKey) // read-only key
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "revoke_reservation",
		Arguments: map[string]any{"reservationId": resID},
	})
	if err != nil {
		t.Fatalf("call tool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("revoke_reservation with a read-only key must be denied (scope gate)")
	}
}

func TestServer_RevokeReservationSucceedsForWriteKey(t *testing.T) {
	url, resID := newWriteServer(t)
	session := connect(t, url, writeKey) // read-write key
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "revoke_reservation",
		Arguments: map[string]any{"reservationId": resID},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("revoke_reservation with write key returned error: %+v", res.Content)
	}
	revoked, ok := res.StructuredContent.(map[string]any)["revoked"]
	if !ok || revoked != true {
		t.Fatalf("expected revoked=true, got %+v", res.StructuredContent)
	}

	// A second revocation over the wire is rejected (already revoked).
	again, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "revoke_reservation",
		Arguments: map[string]any{"reservationId": resID},
	})
	if err != nil {
		t.Fatalf("second call transport error: %v", err)
	}
	if !again.IsError {
		t.Fatal("second revoke_reservation must be rejected")
	}
}
