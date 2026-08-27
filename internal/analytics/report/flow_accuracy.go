// Package report holds the inventory-storage "Inventory Flow & Accuracy" read
// model: the shapes of the analytical report the data product serves, the
// query that selects it, and the outbound ports the writer and reader
// adapters implement. It is a read-model region that depends on nothing else
// in this module — the OLTP domain and application layers must not import it,
// and it must not import them (ADR-0011).
package report

import "time"

// Granularity is the time-bucket resolution a report is rolled up to. Only
// hourly buckets are modelled for this round.
type Granularity string

const (
	// GranularityHour rolls rows up into UTC hour buckets.
	GranularityHour Granularity = "hour"
)

// RowKey identifies a single Inventory Flow & Accuracy row: the SKU, the bin,
// and the UTC hour bucket the row aggregates. Flow events (received, picked,
// reserved) carry a SKU but no bin; accuracy events (cycle count, discrepancy)
// carry a bin but no SKU; stow and unlocate carry both. The unused dimension
// is the empty string for a given event, so a row is keyed by whichever
// dimension(s) its events actually carry. HourBucket is the bucket start,
// truncated to the hour in UTC.
type RowKey struct {
	SKU        string
	BinId      string
	HourBucket time.Time
}

// Row is one aggregated Inventory Flow & Accuracy row for a (sku, binId,
// hourBucket) key. Flow metrics that carry a quantity value object use the
// summed quantity; the rest are event counts.
type Row struct {
	Key RowKey
	// ReceivedQuantity is the summed quantity of StockReceived in this bucket.
	ReceivedQuantity int
	// StowedCount is the number of ItemStowed events in this bucket.
	StowedCount int
	// PickedQuantity is the summed quantity of StockPicked in this bucket.
	PickedQuantity int
	// ReservationsCreated is the number of StockReserved events in this bucket.
	ReservationsCreated int
	// ReservationsExpired is the number of ReservationExpired events in this
	// bucket.
	ReservationsExpired int
	// ReservationsRevoked is the number of ReservationRevoked events in this
	// bucket.
	ReservationsRevoked int
	// CycleCountsCompleted is the number of CycleCountCompleted events in this
	// bucket.
	CycleCountsCompleted int
	// DiscrepanciesDetected is the number of DiscrepancyDetected events in this
	// bucket.
	DiscrepanciesDetected int
	// UnlocatedCount is the number of ItemUnlocated events in this bucket.
	UnlocatedCount int
}

// FlowAccuracyReport is the full result of a report query: the matching rows.
type FlowAccuracyReport struct {
	Rows []Row
}

// ReportQuery selects and filters the rows a report covers. From is inclusive
// and To is exclusive, both compared against a row's HourBucket. SKU and BinId
// are optional exact-match filters (empty means "no filter on this
// dimension").
type ReportQuery struct {
	From        time.Time
	To          time.Time
	SKU         string
	BinId       string
	Granularity Granularity
}
