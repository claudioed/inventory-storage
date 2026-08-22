package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	inboundhttp "github.com/claudioed/inventory-storage/internal/adapters/inbound/http"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/events"
	"github.com/claudioed/inventory-storage/internal/adapters/outbound/memory"
	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/location"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

type testServer struct {
	handler   http.Handler
	stock     *memory.StockRepo
	locations *memory.LocationRepo
}

func newTestServer() testServer {
	stockRepo := memory.NewStockRepo()
	locationRepo := memory.NewLocationRepo()
	reservationRepo := memory.NewReservationRepo()
	publisher := events.NewBufferedPublisher()
	clock := memory.NewFixedClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	s := &inboundhttp.Server{
		ReceiveStock:      &usecases.ReceiveStock{Events: publisher, Clock: clock},
		StowStock:         &usecases.StowStock{Stock: stockRepo, Locations: locationRepo, Events: publisher, Clock: clock},
		ReserveStock:      &usecases.ReserveStock{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		RevokeReservation: &usecases.RevokeReservation{Stock: stockRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		ConfirmPick:       &usecases.ConfirmPick{Stock: stockRepo, Locations: locationRepo, Reservations: reservationRepo, Events: publisher, Clock: clock},
		GetUsable:         &usecases.GetUsable{Stock: stockRepo},
		RunCycleCount:     &usecases.RunCycleCount{Stock: stockRepo, Events: publisher, Clock: clock},
	}

	return testServer{handler: inboundhttp.NewRouter(s), stock: stockRepo, locations: locationRepo}
}

func (ts testServer) seedBin(t *testing.T, id string, capacity int) {
	t.Helper()
	binID, _ := shared.NewBinId(id)
	cap, _ := shared.NewQuantity(capacity)
	bin, err := location.NewBin(binID, cap)
	if err != nil {
		t.Fatalf("unexpected error seeding bin: %v", err)
	}
	if err := ts.locations.Save(context.Background(), bin); err != nil {
		t.Fatalf("unexpected error saving bin: %v", err)
	}
}

func (ts testServer) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("unexpected error marshaling body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	ts.handler.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	ts := newTestServer()
	rec := ts.do(t, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestReceiveStock_Endpoint(t *testing.T) {
	ts := newTestServer()
	rec := ts.do(t, http.MethodPost, "/stock/receive", map[string]any{"sku": "SKU-1", "quantity": 10})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReceiveStock_Endpoint_InvalidQuantity(t *testing.T) {
	ts := newTestServer()
	rec := ts.do(t, http.MethodPost, "/stock/receive", map[string]any{"sku": "SKU-1", "quantity": 0})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStowStock_Endpoint(t *testing.T) {
	ts := newTestServer()
	ts.seedBin(t, "A-1-1", 10)

	rec := ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 5, "binId": "A-1-1"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc == "" || loc == "/stock/" {
		t.Fatalf("expected non-empty Location header pointing at the created stock unit, got %q", loc)
	}
}

func TestStowStock_Endpoint_CapacityExceeded(t *testing.T) {
	ts := newTestServer()
	ts.seedBin(t, "A-1-1", 5)

	rec := ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 6, "binId": "A-1-1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusConflict, "bin-full", "/stock/stow")
}

func TestStowStock_Endpoint_UnknownBin(t *testing.T) {
	ts := newTestServer()
	rec := ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 6, "binId": "A-1-1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReservationLifecycle_Endpoints(t *testing.T) {
	ts := newTestServer()
	ts.seedBin(t, "A-1-1", 10)
	ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 10, "binId": "A-1-1"})

	reserveRec := ts.do(t, http.MethodPost, "/reservations", map[string]any{"sku": "SKU-1", "quantity": 6, "demandRef": "order-1"})
	if reserveRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", reserveRec.Code, reserveRec.Body.String())
	}
	var reserved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(reserveRec.Body.Bytes(), &reserved); err != nil {
		t.Fatalf("unexpected error decoding response: %v", err)
	}
	if loc := reserveRec.Header().Get("Location"); loc != "/reservations/"+reserved.ID {
		t.Fatalf("expected Location header /reservations/%s, got %q", reserved.ID, loc)
	}

	usableRec := ts.do(t, http.MethodGet, "/inventory/SKU-1/usable", nil)
	if usableRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", usableRec.Code, usableRec.Body.String())
	}

	confirmRec := ts.do(t, http.MethodPost, "/reservations/"+reserved.ID+"/confirm-pick", nil)
	if confirmRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", confirmRec.Code, confirmRec.Body.String())
	}
}

func TestReserveStock_Endpoint_InsufficientUsable(t *testing.T) {
	ts := newTestServer()
	ts.seedBin(t, "A-1-1", 5)
	ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 5, "binId": "A-1-1"})

	rec := ts.do(t, http.MethodPost, "/reservations", map[string]any{"sku": "SKU-1", "quantity": 6, "demandRef": "order-1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeReservation_Endpoint(t *testing.T) {
	ts := newTestServer()
	ts.seedBin(t, "A-1-1", 10)
	ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 10, "binId": "A-1-1"})
	reserveRec := ts.do(t, http.MethodPost, "/reservations", map[string]any{"sku": "SKU-1", "quantity": 6, "demandRef": "order-1"})
	var reserved struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(reserveRec.Body.Bytes(), &reserved)

	rec := ts.do(t, http.MethodDelete, "/reservations/"+reserved.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeReservation_Endpoint_UnknownID(t *testing.T) {
	ts := newTestServer()
	rec := ts.do(t, http.MethodDelete, "/reservations/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusNotFound, "reservation-not-found", "/reservations/does-not-exist")
}

func TestReceiveStock_Endpoint_MalformedBody(t *testing.T) {
	ts := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/stock/receive", bytes.NewReader([]byte("{not-json")))
	rec := httptest.NewRecorder()
	ts.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProblemDetails(t, rec, http.StatusBadRequest, "malformed-request-body", "/stock/receive")
}

// problemBody mirrors the RFC 7807 (application/problem+json) shape this
// service's error responses use.
type problemBody struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}

func assertProblemDetails(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantSlug, wantInstance string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected Content-Type application/problem+json, got %q", ct)
	}
	var p problemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unexpected error decoding problem details: %v (body: %s)", err, rec.Body.String())
	}
	wantType := "https://errors.inventory-storage.warehouse-systems.dev/" + wantSlug
	if p.Type != wantType {
		t.Fatalf("expected type %q, got %q", wantType, p.Type)
	}
	if p.Title == "" {
		t.Fatalf("expected non-empty title, got empty")
	}
	if p.Status != wantStatus {
		t.Fatalf("expected status %d in body, got %d", wantStatus, p.Status)
	}
	if p.Detail == "" {
		t.Fatalf("expected non-empty detail, got empty")
	}
	if p.Instance != wantInstance {
		t.Fatalf("expected instance %q, got %q", wantInstance, p.Instance)
	}
}

func TestGetUsable_Endpoint(t *testing.T) {
	ts := newTestServer()
	ts.seedBin(t, "A-1-1", 10)
	ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 7, "binId": "A-1-1"})

	rec := ts.do(t, http.MethodGet, "/inventory/SKU-1/usable", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Usable int `json:"usable"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Usable != 7 {
		t.Fatalf("expected usable=7, got %d", body.Usable)
	}
}

func TestRunCycleCount_Endpoint(t *testing.T) {
	ts := newTestServer()
	ts.seedBin(t, "A-1-1", 10)
	ts.do(t, http.MethodPost, "/stock/stow", map[string]any{"sku": "SKU-1", "quantity": 10, "binId": "A-1-1"})

	rec := ts.do(t, http.MethodPost, "/bins/A-1-1/cycle-count", map[string]any{"countedQuantity": 10})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
