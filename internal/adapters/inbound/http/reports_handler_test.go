package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/analytics/report"
)

// stubReportStore is a test double for report.ReportStore.
type stubReportStore struct {
	rep      report.FlowAccuracyReport
	lag      time.Duration
	queryErr error
	lastQ    report.ReportQuery
}

func (s *stubReportStore) Query(_ context.Context, q report.ReportQuery) (report.FlowAccuracyReport, error) {
	s.lastQ = q
	return s.rep, s.queryErr
}

func (s *stubReportStore) FreshnessLag(context.Context) (time.Duration, error) {
	return s.lag, nil
}

func TestReportsHandler_GetFlowAccuracy(t *testing.T) {
	bucket := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	store := &stubReportStore{
		rep: report.FlowAccuracyReport{Rows: []report.Row{
			{Key: report.RowKey{SKU: "SKU-1", HourBucket: bucket}, ReceivedQuantity: 42, PickedQuantity: 7},
			{Key: report.RowKey{BinId: "BIN-A", HourBucket: bucket}, CycleCountsCompleted: 1, DiscrepanciesDetected: 1},
		}},
	}
	h := &ReportsHandlers{Store: store}
	router := NewReportsRouter(h, nil, "test")

	req := httptest.NewRequest(http.MethodGet,
		"/reports/flow-accuracy?from=2026-08-26T00:00:00Z&to=2026-08-27T00:00:00Z&sku=SKU-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if store.lastQ.SKU != "SKU-1" {
		t.Errorf("sku filter not forwarded: %+v", store.lastQ)
	}
	var body flowAccuracyReportDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(body.Rows))
	}
	if body.Rows[0].ReceivedQuantity != 42 || body.Rows[0].HourBucket != "2026-08-26T14:00:00Z" {
		t.Errorf("row0 = %+v", body.Rows[0])
	}
}

func TestReportsHandler_MissingRequiredParams(t *testing.T) {
	h := &ReportsHandlers{Store: &stubReportStore{}}
	router := NewReportsRouter(h, nil, "test")

	tests := []string{
		"/reports/flow-accuracy",
		"/reports/flow-accuracy?from=2026-08-26T00:00:00Z",
		"/reports/flow-accuracy?from=not-a-time&to=2026-08-27T00:00:00Z",
		"/reports/flow-accuracy?from=2026-08-26T00:00:00Z&to=2026-08-27T00:00:00Z&granularity=day",
	}
	for _, url := range tests {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", url, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: content-type = %q, want application/problem+json", url, ct)
		}
	}
}

func TestReportsHandler_GetFreshness(t *testing.T) {
	h := &ReportsHandlers{Store: &stubReportStore{lag: 4500 * time.Millisecond}}
	router := NewReportsRouter(h, nil, "test")

	req := httptest.NewRequest(http.MethodGet, "/reports/flow-accuracy/freshness", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body freshnessDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.LagSeconds != 4.5 {
		t.Errorf("lagSeconds = %v, want 4.5", body.LagSeconds)
	}
}

func TestReportsHandler_Healthz(t *testing.T) {
	h := &ReportsHandlers{Store: &stubReportStore{}}
	router := NewReportsRouter(h, nil, "test")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
