// Package analyticsstore provides the outbound adapters that persist and serve
// the inventory-storage "Inventory Flow & Accuracy" read model: an in-memory
// implementation (MemoryStore) for tests and local runs, and Postgres
// implementations (a writer projection and a read-only reader) for deployment.
// All satisfy the report.ProjectionStore and/or report.ReportStore ports.
package analyticsstore

import (
	"context"
	"sync"
	"time"

	"github.com/claudioed/inventory-storage/internal/analytics/report"
)

// MemoryStore is an in-memory implementation of both report.ProjectionStore
// (write) and report.ReportStore (read), backed by maps. It is idempotent per
// eventId via a seen-set, so a duplicate delivery is a no-op. It is safe for
// concurrent use.
type MemoryStore struct {
	// Now supplies the current time for FreshnessLag; defaults to time.Now
	// when nil so lag is deterministic under test.
	Now func() time.Time

	mu   sync.Mutex
	seen map[string]struct{}
	rows map[report.RowKey]*rowAcc
	// latest is the OccurredAt of the most recently applied event, used to
	// compute FreshnessLag.
	latest time.Time
}

// rowAcc accumulates the running totals for one report row.
type rowAcc struct {
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

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		seen: map[string]struct{}{},
		rows: map[report.RowKey]*rowAcc{},
	}
}

func hourBucket(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

// firstApply marks eventId as seen and reports whether this is the first time
// (so the caller should apply the effect) or a duplicate (skip). It also
// advances the freshness watermark. The caller must hold s.mu.
func (s *MemoryStore) firstApply(eventId string, at time.Time) bool {
	if _, dup := s.seen[eventId]; dup {
		return false
	}
	s.seen[eventId] = struct{}{}
	if at.After(s.latest) {
		s.latest = at
	}
	return true
}

func (s *MemoryStore) row(k report.RowKey) *rowAcc {
	r, ok := s.rows[k]
	if !ok {
		r = &rowAcc{}
		s.rows[k] = r
	}
	return r
}

// ApplyStockReceived adds qty to the (sku, hour) row's received quantity.
func (s *MemoryStore) ApplyStockReceived(_ context.Context, eventId, sku string, qty int, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).receivedQuantity += qty
	return nil
}

// ApplyItemStowed increments the (sku, binId, hour) row's stowed count.
func (s *MemoryStore) ApplyItemStowed(_ context.Context, eventId, sku, binId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, BinId: binId, HourBucket: hourBucket(at)}).stowedCount++
	return nil
}

// ApplyStockPicked adds qty to the (sku, hour) row's picked quantity.
func (s *MemoryStore) ApplyStockPicked(_ context.Context, eventId, sku string, qty int, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).pickedQuantity += qty
	return nil
}

// ApplyStockReserved increments the (sku, hour) row's created-reservation count.
func (s *MemoryStore) ApplyStockReserved(_ context.Context, eventId, sku string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).reservationsCreated++
	return nil
}

// ApplyReservationExpired increments the (sku, hour) row's expired count.
func (s *MemoryStore) ApplyReservationExpired(_ context.Context, eventId, sku string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).reservationsExpired++
	return nil
}

// ApplyReservationRevoked increments the (sku, hour) row's revoked count.
func (s *MemoryStore) ApplyReservationRevoked(_ context.Context, eventId, sku string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, HourBucket: hourBucket(at)}).reservationsRevoked++
	return nil
}

// ApplyCycleCountCompleted increments the (binId, hour) row's cycle-count count.
func (s *MemoryStore) ApplyCycleCountCompleted(_ context.Context, eventId, binId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{BinId: binId, HourBucket: hourBucket(at)}).cycleCountsCompleted++
	return nil
}

// ApplyDiscrepancyDetected increments the (binId, hour) row's discrepancy count.
func (s *MemoryStore) ApplyDiscrepancyDetected(_ context.Context, eventId, binId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{BinId: binId, HourBucket: hourBucket(at)}).discrepanciesDetected++
	return nil
}

// ApplyItemUnlocated increments the (sku, binId, hour) row's unlocated count.
func (s *MemoryStore) ApplyItemUnlocated(_ context.Context, eventId, sku, binId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{SKU: sku, BinId: binId, HourBucket: hourBucket(at)}).unlocatedCount++
	return nil
}

// Query returns the rows matching q. From is inclusive, To is exclusive, both
// compared against a row's HourBucket; empty SKU/BinId means no filter on that
// dimension.
func (s *MemoryStore) Query(_ context.Context, q report.ReportQuery) (report.FlowAccuracyReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// FreshnessLag returns how far the read model lags real time: now minus the
// OccurredAt of the most recently applied event. Zero when nothing has been
// applied yet, and never negative (a future-dated event clamps to zero).
func (s *MemoryStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.latest.IsZero() {
		return 0, nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	lag := now.Sub(s.latest)
	if lag < 0 {
		return 0, nil
	}
	return lag, nil
}

// Compile-time assertions that MemoryStore satisfies both ports.
var (
	_ report.ProjectionStore = (*MemoryStore)(nil)
	_ report.ReportStore     = (*MemoryStore)(nil)
)
