package http_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/adapters/inbound/auth"
	inboundhttp "github.com/claudioed/inventory-storage/internal/adapters/inbound/http"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
)

const (
	testReadKey      = "test-read-key"
	testReadWriteKey = "test-rw-key"
)

func authTestServer() *inboundhttp.Server {
	stockRepo := memory.NewStockRepo()
	publisher := events.NewBufferedPublisher()
	clock := memory.NewFixedClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return &inboundhttp.Server{
		ReceiveStock:    &usecases.ReceiveStock{Events: publisher, Clock: clock},
		GetUsable:       &usecases.GetUsable{Stock: stockRepo},
		Classifications: memory.NewProductClassificationRepo(),
	}
}

// authRouter builds the real OLTP router with the fleet REST identity
// middleware in the given mode, the same way cmd/inventory wires it.
func authRouter(mode auth.Mode) http.Handler {
	authn := auth.NewStaticKeyAuth(map[string]auth.Scope{testReadKey: auth.ScopeRead, testReadWriteKey: auth.ScopeReadWrite})
	return inboundhttp.NewRouter(authTestServer(), slog.New(slog.NewTextHandler(io.Discard, nil)), "",
		inboundhttp.WithAuth(auth.Middleware{Authn: authn, Mode: mode}))
}

func authDo(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	var rd io.Reader = http.NoBody
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, slug string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var problem struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("problem body is not JSON: %v", err)
	}
	if problem.Type != "https://errors.inventory-storage.warehouse-systems.dev/"+slug || problem.Status != status {
		t.Fatalf("problem = %+v, want type slug %q status %d", problem, slug, status)
	}
}

// TestRESTAuth_EnforceTable is the fleet's router table test (ADR-0014): one
// GET and one mutating route, every scope combination, and /healthz open.
func TestRESTAuth_EnforceTable(t *testing.T) {
	h := authRouter(auth.ModeEnforce)
	const receiveBody = `{"sku":"SKU-1","quantity":1}`

	t.Run("no token on GET -> 401 problem+json with WWW-Authenticate", func(t *testing.T) {
		rec := authDo(h, http.MethodGet, "/inventory/SKU-1/usable", "", "")
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
		if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer ") {
			t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", got)
		}
	})
	t.Run("no token on POST -> 401", func(t *testing.T) {
		assertProblem(t, authDo(h, http.MethodPost, "/stock/receive", "", receiveBody), http.StatusUnauthorized, "unauthenticated")
	})
	t.Run("bogus token -> 401", func(t *testing.T) {
		assertProblem(t, authDo(h, http.MethodGet, "/inventory/SKU-1/usable", "nope", ""), http.StatusUnauthorized, "unauthenticated")
	})
	t.Run("read key on GET -> 200", func(t *testing.T) {
		if rec := authDo(h, http.MethodGet, "/inventory/SKU-1/usable", testReadKey, ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("read key on POST -> 403 insufficient-scope", func(t *testing.T) {
		assertProblem(t, authDo(h, http.MethodPost, "/stock/receive", testReadKey, receiveBody), http.StatusForbidden, "insufficient-scope")
	})
	t.Run("read-write key on POST -> 2xx", func(t *testing.T) {
		if rec := authDo(h, http.MethodPost, "/stock/receive", testReadWriteKey, receiveBody); rec.Code/100 != 2 {
			t.Fatalf("status = %d, want 2xx (body %s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("/healthz with no token -> 200", func(t *testing.T) {
		if rec := authDo(h, http.MethodGet, "/healthz", "", ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
	t.Run("every route group is behind the middleware", func(t *testing.T) {
		for _, path := range []string{"/reservations?demandRef=x", "/products/SKU-1/classification"} {
			if rec := authDo(h, http.MethodGet, path, "", ""); rec.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s without a token: status = %d, want 401", path, rec.Code)
			}
		}
	})
}

func TestRESTAuth_LogModePassesAndOffIsNoop(t *testing.T) {
	if rec := authDo(authRouter(auth.ModeLog), http.MethodGet, "/inventory/SKU-1/usable", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("log mode must let an unauthenticated GET through, got %d", rec.Code)
	}
	if rec := authDo(authRouter(auth.ModeOff), http.MethodGet, "/inventory/SKU-1/usable", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("off mode must be a no-op, got %d", rec.Code)
	}
}

func TestRESTAuth_ReportsRouterRequiresReadOnEveryRoute(t *testing.T) {
	authn := auth.NewStaticKeyAuth(map[string]auth.Scope{testReadKey: auth.ScopeRead})
	h := inboundhttp.NewReportsRouter(&inboundhttp.ReportsHandlers{}, slog.New(slog.NewTextHandler(io.Discard, nil)), "",
		inboundhttp.WithAuth(auth.Middleware{Authn: authn, Mode: auth.ModeEnforce}))
	if rec := authDo(h, http.MethodGet, "/reports/flow-accuracy/freshness", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}
	if rec := authDo(h, http.MethodGet, "/healthz", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("/healthz: status = %d, want 200", rec.Code)
	}
	// With the read key the middleware admits the request; whatever the
	// handler does with a nil store is not this test's concern.
	if rec := authDo(h, http.MethodGet, "/reports/flow-accuracy/freshness", testReadKey, ""); rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("read key must be admitted, got %d", rec.Code)
	}
}
