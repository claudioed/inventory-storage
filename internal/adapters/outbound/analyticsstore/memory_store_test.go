package analyticsstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/analyticsstore"
	"github.com/claudioed/inventory-storage/internal/analytics/report"
)

func TestMemoryStore_ProjectsAndQueries(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	s := analyticsstore.NewMemoryStore()
	ctx := context.Background()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
	}

	must(s.ApplyStockReceived(ctx, "e1", "SKU-1", 10, base))
	must(s.ApplyStockPicked(ctx, "e2", "SKU-1", 3, base.Add(time.Minute)))
	must(s.ApplyItemStowed(ctx, "e3", "SKU-1", "BIN-A", base.Add(2*time.Minute)))
	must(s.ApplyCycleCountCompleted(ctx, "e4", "BIN-A", base.Add(3*time.Minute)))
	must(s.ApplyDiscrepancyDetected(ctx, "e5", "BIN-A", base.Add(4*time.Minute)))

	rep, err := s.Query(ctx, report.ReportQuery{
		From: base.Add(-time.Hour), To: base.Add(time.Hour), Granularity: report.GranularityHour,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	bucket := base.Truncate(time.Hour)
	skuRow := findRow(rep, report.RowKey{SKU: "SKU-1", HourBucket: bucket})
	if skuRow == nil || skuRow.ReceivedQuantity != 10 || skuRow.PickedQuantity != 3 {
		t.Errorf("sku row = %+v", skuRow)
	}
	binRow := findRow(rep, report.RowKey{BinId: "BIN-A", HourBucket: bucket})
	if binRow == nil || binRow.CycleCountsCompleted != 1 || binRow.DiscrepanciesDetected != 1 {
		t.Errorf("bin row = %+v", binRow)
	}
}

func TestMemoryStore_IdempotentOnEventId(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	s := analyticsstore.NewMemoryStore()
	ctx := context.Background()

	for range 3 {
		if err := s.ApplyStockReceived(ctx, "dup", "SKU-1", 5, base); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	rep, err := s.Query(ctx, report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), Granularity: report.GranularityHour})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	row := findRow(rep, report.RowKey{SKU: "SKU-1", HourBucket: base.Truncate(time.Hour)})
	if row == nil || row.ReceivedQuantity != 5 {
		t.Errorf("expected received=5 after 3 duplicate applies, got %+v", row)
	}
}

func TestMemoryStore_FreshnessLag(t *testing.T) {
	s := analyticsstore.NewMemoryStore()
	ctx := context.Background()

	// Empty store: zero lag.
	if lag, err := s.FreshnessLag(ctx); err != nil || lag != 0 {
		t.Fatalf("empty-store lag = %v, err = %v; want 0, nil", lag, err)
	}

	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	if err := s.ApplyStockReserved(ctx, "e1", "SKU-1", now.Add(-90*time.Second)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	lag, err := s.FreshnessLag(ctx)
	if err != nil {
		t.Fatalf("FreshnessLag: %v", err)
	}
	if lag != 90*time.Second {
		t.Errorf("lag = %v, want 90s", lag)
	}
}

func findRow(rep report.FlowAccuracyReport, k report.RowKey) *report.Row {
	for i := range rep.Rows {
		if rep.Rows[i].Key == k {
			return &rep.Rows[i]
		}
	}
	return nil
}
