package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/inventory-storage/internal/analytics/report"
)

// fakeStore is an in-memory implementation of both report ports used to
// exercise report derivation from a synthetic event sequence. It is a test
// double local to this package: the production stores live in the
// analyticsstore outbound adapter.
type fakeStore struct {
	seen map[string]bool
	rows map[report.RowKey]*acc
}

// acc is the fake store's per-row accumulator, kept separate from the public
// report.Row so the running-total intermediate state never leaks into the
// read-model type.
type acc struct {
	receivedQuantity      int
	stowedCount           int
	pickedQuantity        int
	reservationsCreated   int
	reservationsExpired   int
	reservationsRevoked   int
	cycleCountsCompleted  int
	discrepanciesDetected int
	unlocatedCount        int
}

func newFakeStore() *fakeStore {
	return &fakeStore{seen: map[string]bool{}, rows: map[report.RowKey]*acc{}}
}

func (s *fakeStore) row(k report.RowKey) *acc {
	r, ok := s.rows[k]
	if !ok {
		r = &acc{}
		s.rows[k] = r
	}
	return r
}

func (s *fakeStore) dup(eventId string) bool {
	if s.seen[eventId] {
		return true
	}
	s.seen[eventId] = true
	return false
}

func hourBucket(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

func (s *fakeStore) ApplyStockReceived(_ context.Context, eventId, sku string, qty int, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).receivedQuantity += qty
	return nil
}

func (s *fakeStore) ApplyItemStowed(_ context.Context, eventId, sku, binId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, BinId: binId, HourBucket: hourBucket(at)}).stowedCount++
	return nil
}

func (s *fakeStore) ApplyStockPicked(_ context.Context, eventId, sku string, qty int, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).pickedQuantity += qty
	return nil
}

func (s *fakeStore) ApplyStockReserved(_ context.Context, eventId, sku string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).reservationsCreated++
	return nil
}

func (s *fakeStore) ApplyReservationExpired(_ context.Context, eventId, sku string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).reservationsExpired++
	return nil
}

func (s *fakeStore) ApplyReservationRevoked(_ context.Context, eventId, sku string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).reservationsRevoked++
	return nil
}

func (s *fakeStore) ApplyCycleCountCompleted(_ context.Context, eventId, binId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{BinId: binId, HourBucket: hourBucket(at)}).cycleCountsCompleted++
	return nil
}

func (s *fakeStore) ApplyDiscrepancyDetected(_ context.Context, eventId, binId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{BinId: binId, HourBucket: hourBucket(at)}).discrepanciesDetected++
	return nil
}

func (s *fakeStore) ApplyItemUnlocated(_ context.Context, eventId, sku, binId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, BinId: binId, HourBucket: hourBucket(at)}).unlocatedCount++
	return nil
}

func (s *fakeStore) Query(_ context.Context, q report.ReportQuery) (report.FlowAccuracyReport, error) {
	out := report.FlowAccuracyReport{}
	for k, r := range s.rows {
		if k.HourBucket.Before(q.From) || !k.HourBucket.Before(q.To) {
			continue
		}
		if q.SKU != "" && k.SKU != q.SKU {
			continue
		}
		if q.BinId != "" && k.BinId != q.BinId {
			continue
		}
		out.Rows = append(out.Rows, report.Row{
			Key:                   k,
			ReceivedQuantity:      r.receivedQuantity,
			StowedCount:           r.stowedCount,
			PickedQuantity:        r.pickedQuantity,
			ReservationsCreated:   r.reservationsCreated,
			ReservationsExpired:   r.reservationsExpired,
			ReservationsRevoked:   r.reservationsRevoked,
			CycleCountsCompleted:  r.cycleCountsCompleted,
			DiscrepanciesDetected: r.discrepanciesDetected,
			UnlocatedCount:        r.unlocatedCount,
		})
	}
	return out, nil
}

func (s *fakeStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	return 0, nil
}

func TestFlowAccuracyReport_DerivesFromEventSequence(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s := newFakeStore()
	ctx := context.Background()

	// Synthetic sequence for one SKU and one bin in one hour bucket.
	must(t, s.ApplyStockReceived(ctx, "e1", "SKU-1", 10, base))
	must(t, s.ApplyStockReceived(ctx, "e2", "SKU-1", 5, base.Add(time.Minute)))
	must(t, s.ApplyItemStowed(ctx, "e3", "SKU-1", "BIN-A", base.Add(2*time.Minute)))
	must(t, s.ApplyStockReserved(ctx, "e4", "SKU-1", base.Add(3*time.Minute)))
	must(t, s.ApplyStockPicked(ctx, "e5", "SKU-1", 4, base.Add(4*time.Minute)))
	must(t, s.ApplyReservationExpired(ctx, "e6", "SKU-1", base.Add(5*time.Minute)))
	must(t, s.ApplyReservationRevoked(ctx, "e7", "SKU-1", base.Add(6*time.Minute)))
	must(t, s.ApplyItemUnlocated(ctx, "e8", "SKU-1", "BIN-A", base.Add(7*time.Minute)))
	// Accuracy events keyed by bin only.
	must(t, s.ApplyCycleCountCompleted(ctx, "e9", "BIN-A", base.Add(8*time.Minute)))
	must(t, s.ApplyDiscrepancyDetected(ctx, "e10", "BIN-A", base.Add(9*time.Minute)))

	rep, err := s.Query(ctx, report.ReportQuery{
		From:        base.Add(-time.Hour),
		To:          base.Add(2 * time.Hour),
		Granularity: report.GranularityHour,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	bucket := base.Truncate(time.Hour)

	// SKU-only flow row (received, picked, reservations).
	skuRow := findRow(rep, report.RowKey{SKU: "SKU-1", HourBucket: bucket})
	if skuRow == nil {
		t.Fatal("no SKU-1 (no-bin) row")
	}
	if skuRow.ReceivedQuantity != 15 {
		t.Errorf("ReceivedQuantity = %d, want 15", skuRow.ReceivedQuantity)
	}
	if skuRow.PickedQuantity != 4 {
		t.Errorf("PickedQuantity = %d, want 4", skuRow.PickedQuantity)
	}
	if skuRow.ReservationsCreated != 1 {
		t.Errorf("ReservationsCreated = %d, want 1", skuRow.ReservationsCreated)
	}
	if skuRow.ReservationsExpired != 1 {
		t.Errorf("ReservationsExpired = %d, want 1", skuRow.ReservationsExpired)
	}
	if skuRow.ReservationsRevoked != 1 {
		t.Errorf("ReservationsRevoked = %d, want 1", skuRow.ReservationsRevoked)
	}

	// SKU+bin row (stow, unlocate).
	stowRow := findRow(rep, report.RowKey{SKU: "SKU-1", BinId: "BIN-A", HourBucket: bucket})
	if stowRow == nil {
		t.Fatal("no SKU-1/BIN-A row")
	}
	if stowRow.StowedCount != 1 {
		t.Errorf("StowedCount = %d, want 1", stowRow.StowedCount)
	}
	if stowRow.UnlocatedCount != 1 {
		t.Errorf("UnlocatedCount = %d, want 1", stowRow.UnlocatedCount)
	}

	// Bin-only accuracy row.
	binRow := findRow(rep, report.RowKey{BinId: "BIN-A", HourBucket: bucket})
	if binRow == nil {
		t.Fatal("no BIN-A (no-sku) row")
	}
	if binRow.CycleCountsCompleted != 1 {
		t.Errorf("CycleCountsCompleted = %d, want 1", binRow.CycleCountsCompleted)
	}
	if binRow.DiscrepanciesDetected != 1 {
		t.Errorf("DiscrepanciesDetected = %d, want 1", binRow.DiscrepanciesDetected)
	}
}

func TestFlowAccuracyReport_FiltersAndIdempotency(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()

	tests := []struct {
		name  string
		query report.ReportQuery
		want  int // number of rows expected
	}{
		{"no filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), Granularity: report.GranularityHour}, 2},
		{"sku filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), SKU: "SKU-1", Granularity: report.GranularityHour}, 1},
		{"bin filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), BinId: "BIN-B", Granularity: report.GranularityHour}, 1},
		{"window excludes all", report.ReportQuery{From: base.Add(24 * time.Hour), To: base.Add(48 * time.Hour), Granularity: report.GranularityHour}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newFakeStore()
			// Apply the same receive twice with the same eventId → counts once.
			must(t, s.ApplyStockReceived(ctx, "dup", "SKU-1", 7, base))
			must(t, s.ApplyStockReceived(ctx, "dup", "SKU-1", 7, base))
			// A bin-only accuracy row in the same bucket.
			must(t, s.ApplyCycleCountCompleted(ctx, "other", "BIN-B", base))

			rep, err := s.Query(ctx, tt.query)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(rep.Rows) != tt.want {
				t.Errorf("rows = %d, want %d", len(rep.Rows), tt.want)
			}
			if tt.name == "no filter" {
				sku := findRow(rep, report.RowKey{SKU: "SKU-1", HourBucket: base.Truncate(time.Hour)})
				if sku == nil || sku.ReceivedQuantity != 7 {
					t.Errorf("dedupe failed: SKU-1 received = %v", sku)
				}
			}
		})
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

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
}
