package facilitycache

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/claudioed/inventory-storage/internal/domain/product"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// fakeReader replays a fixed slice of messages and then blocks forever on
// io.EOF, standing in for a real *kafkago.Reader without a broker.
type fakeReader struct {
	msgs []kafkago.Message
	i    int
}

func (f *fakeReader) ReadMessage(_ context.Context) (kafkago.Message, error) {
	if f.i >= len(f.msgs) {
		return kafkago.Message{}, io.EOF
	}
	m := f.msgs[f.i]
	f.i++
	return m, nil
}

func (f *fakeReader) Close() error { return nil }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// envelopeMsg builds one Kafka message carrying facility-layout's
// CloudEvents-like envelope, using the FULLY-QUALIFIED event type that
// service really publishes.
func envelopeMsg(t *testing.T, partition int, offset int64, entity, eventName string, data any) kafkago.Message {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	env := envelope{
		EventType: "com.warehouse.wms.facility-layout." + entity + "." + eventName,
		Data:      raw,
	}
	value, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return kafkago.Message{Partition: partition, Offset: offset, Value: value}
}

// newTestConsumer builds a Consumer around a fakeReader with an explicit
// readiness target, bypassing NewConsumer's broker dial.
func newTestConsumer(msgs []kafkago.Message, target targetOffsets) *Consumer {
	return &Consumer{
		Reader:  &fakeReader{msgs: msgs},
		Logger:  discardLogger(),
		zones:   make(map[string]product.SlotAttributes),
		slots:   make(map[string]string),
		readyCh: make(chan struct{}),
		target:  target,
	}
}

// drain runs the consumer until the fake reader is exhausted.
func drain(t *testing.T, c *Consumer) {
	t.Helper()
	err := c.Run(context.Background())
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Run: %v", err)
	}
}

func mustBinId(t *testing.T, v string) shared.BinId {
	t.Helper()
	id, err := shared.NewBinId(v)
	if err != nil {
		t.Fatalf("NewBinId(%q): %v", v, err)
	}
	return id
}

func TestGetSlotAttributes_ResolvesSlotThroughItsZone(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-HAZ", TemperatureClass: "Ambient", Hazmat: true,
		}),
		envelopeMsg(t, 0, 1, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-HAZ-A01-01-01-A", ZoneID: "WH1-STOR-HAZ",
		}),
	}, targetOffsets{0: 2})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-HAZ-A01-01-01-A"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	want := product.SlotAttributes{Hazmat: true, TemperatureClass: product.Ambient, Known: true}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// A slot may legitimately be replayed BEFORE the zone it belongs to: they
// are separate aggregates published under different partition keys, so no
// cross-aggregate ordering is guaranteed. The lazy join must cope.
func TestGetSlotAttributes_SlotBeforeZoneStillResolves(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-AMB-A07-03-02-B", ZoneID: "WH1-STOR-AMB",
		}),
		envelopeMsg(t, 0, 1, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-AMB", TemperatureClass: "Chilled", Hazmat: false,
		}),
	}, targetOffsets{0: 2})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-AMB-A07-03-02-B"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if !got.Known || got.TemperatureClass != product.Chilled || got.Hazmat {
		t.Fatalf("got %+v, want Chilled/non-hazmat/known", got)
	}
}

func TestGetSlotAttributes_UnknownSlotIsFailOpen(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-AMB", TemperatureClass: "Ambient",
		}),
	}, targetOffsets{0: 1})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-AMB-Z99-99-99-Z"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if got.Known {
		t.Fatalf("expected Known=false for an unmodeled slot, got %+v", got)
	}
}

// A slot whose zone was never published must report Known=false rather
// than a half-populated answer that would silently look like "no hazmat,
// no temperature constraint".
func TestGetSlotAttributes_SlotWithoutZoneIsNotKnown(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-AMB-A07-03-02-B", ZoneID: "WH1-STOR-AMB",
		}),
	}, targetOffsets{0: 1})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-AMB-A07-03-02-B"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if got.Known {
		t.Fatalf("expected Known=false when the zone is unknown, got %+v", got)
	}
}

func TestDecommissionRemovesSlot(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-AMB", TemperatureClass: "Ambient",
		}),
		envelopeMsg(t, 0, 1, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-AMB-A07-03-02-B", ZoneID: "WH1-STOR-AMB",
		}),
		envelopeMsg(t, 0, 2, "locationslot", "LocationSlotDecommissioned", slotData{
			LocationCode: "WH1-STOR-AMB-A07-03-02-B",
		}),
	}, targetOffsets{0: 3})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-AMB-A07-03-02-B"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if got.Known {
		t.Fatalf("expected a decommissioned slot to be unknown, got %+v", got)
	}
	if c.Slots() != 0 {
		t.Fatalf("expected 0 cached slots, got %d", c.Slots())
	}
}

// A later ZoneRegistered for the same zone must overwrite the earlier one
// (facility-layout is the source of truth; this cache mirrors its latest
// published state).
func TestZoneRegisteredIsLastWriteWins(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-AMB", TemperatureClass: "Ambient", Hazmat: false,
		}),
		envelopeMsg(t, 0, 1, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-AMB-A07-03-02-B", ZoneID: "WH1-STOR-AMB",
		}),
		envelopeMsg(t, 0, 2, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-AMB", TemperatureClass: "Frozen", Hazmat: true,
		}),
	}, targetOffsets{0: 3})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-AMB-A07-03-02-B"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if !got.Hazmat || got.TemperatureClass != product.Frozen {
		t.Fatalf("got %+v, want the revised Frozen/hazmat attributes", got)
	}
}

// An unparseable/empty temperature class must not fail the lookup — the
// zone may simply carry no temperature constraint. Mirrors
// facilitylayout.Client's identical fallback.
func TestUnparseableTemperatureClassFallsBackToZero(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-AMB", TemperatureClass: "", Hazmat: true,
		}),
		envelopeMsg(t, 0, 1, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-AMB-A07-03-02-B", ZoneID: "WH1-STOR-AMB",
		}),
	}, targetOffsets{0: 2})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-AMB-A07-03-02-B"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if !got.Known || !got.Hazmat || got.TemperatureClass != "" {
		t.Fatalf("got %+v, want known/hazmat with an empty temperature class", got)
	}
}

// A LocationSlotRegistered that omits zoneId must still resolve, by
// deriving the zone identity from the code exactly as facility-layout's
// LocationCode.ZoneID() does.
func TestSlotWithoutZoneIdDerivesItFromTheCode(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-HAZ", TemperatureClass: "Ambient", Hazmat: true,
		}),
		envelopeMsg(t, 0, 1, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-HAZ-A01-01-01-A",
		}),
	}, targetOffsets{0: 2})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-HAZ-A01-01-01-A"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if !got.Known || !got.Hazmat {
		t.Fatalf("got %+v, want the derived zone's hazmat attributes", got)
	}
}

func TestZoneIDFromLocationCode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"WH1-STOR-AMB-A07-03-02-B", "WH1-STOR-AMB"},
		{"WH1-STOR-HAZ-A01-01-01-A", "WH1-STOR-HAZ"},
		{"WH1-STOR-AMB", "WH1-STOR-AMB"},
		{"WH1-STOR", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := zoneIDFromLocationCode(tc.in); got != tc.want {
			t.Fatalf("zoneIDFromLocationCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Event types are matched on the trailing name so an upstream namespace
// change cannot silently stop the cache from updating.
func TestEventNameStripsTheCloudEventsNamespace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"com.warehouse.wms.facility-layout.zone.ZoneRegistered", "ZoneRegistered"},
		{"com.warehouse.wms.facility-layout.locationslot.LocationSlotDecommissioned", "LocationSlotDecommissioned"},
		{"ZoneRegistered", "ZoneRegistered"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := eventName(tc.in); got != tc.want {
			t.Fatalf("eventName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Events this cache does not need must be ignored without error AND must
// still advance readiness — otherwise a topic whose final message is, say,
// a PlacementRuleDefined would never let the consumer become ready.
func TestIrrelevantEventsAreIgnoredButStillAdvanceReadiness(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "site", "SiteRegistered", map[string]any{
			"siteCode": "WH1", "siteName": "Warehouse One",
		}),
		envelopeMsg(t, 0, 1, "placementrule", "PlacementRuleDefined", map[string]any{
			"ruleId": "RULE-HAZ-1", "locationType": "PalletRack",
		}),
	}, targetOffsets{0: 2})
	drain(t, c)

	if !c.Ready() {
		t.Fatal("expected the consumer to be ready after reaching its target offset")
	}
	if c.Slots() != 0 || c.Zones() != 0 {
		t.Fatalf("expected an empty cache, got %d slots / %d zones", c.Slots(), c.Zones())
	}
}

// A single malformed message must never stop the cache from tracking
// everything else.
func TestMalformedMessageDoesNotStopTheReplay(t *testing.T) {
	bad := kafkago.Message{Partition: 0, Offset: 0, Value: []byte("{not json")}
	c := newTestConsumer([]kafkago.Message{
		bad,
		envelopeMsg(t, 0, 1, "zone", "ZoneRegistered", zoneData{
			ZoneID: "WH1-STOR-AMB", TemperatureClass: "Ambient",
		}),
		envelopeMsg(t, 0, 2, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "WH1-STOR-AMB-A07-03-02-B", ZoneID: "WH1-STOR-AMB",
		}),
	}, targetOffsets{0: 3})
	drain(t, c)

	got, err := c.GetSlotAttributes(context.Background(), mustBinId(t, "WH1-STOR-AMB-A07-03-02-B"))
	if err != nil {
		t.Fatalf("GetSlotAttributes: %v", err)
	}
	if !got.Known {
		t.Fatalf("expected the later valid events to still apply, got %+v", got)
	}
	if !c.Ready() {
		t.Fatal("expected readiness to advance past a malformed message")
	}
}

func TestReadyOnlyAfterEveryTrackedPartitionReachesItsTarget(t *testing.T) {
	c := newTestConsumer(nil, targetOffsets{0: 2, 1: 1})

	if c.Ready() {
		t.Fatal("must not be ready before consuming anything")
	}
	c.observe(0, 0)
	if c.Ready() {
		t.Fatal("must not be ready with partition 0 short of its target")
	}
	c.observe(0, 1) // offset+1 == 2, partition 0 done
	if c.Ready() {
		t.Fatal("must not be ready while partition 1 is outstanding")
	}
	c.observe(1, 0) // offset+1 == 1, partition 1 done
	if !c.Ready() {
		t.Fatal("expected ready once every tracked partition reached its target")
	}
}

func TestWaitReadyReturnsImmediatelyWhenAlreadyReady(t *testing.T) {
	c := newTestConsumer(nil, targetOffsets{})
	c.markReady()

	if err := c.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
}

func TestWaitReadyHonoursContextCancellation(t *testing.T) {
	c := newTestConsumer(nil, targetOffsets{0: 5})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.WaitReady(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitReady = %v, want context.Canceled", err)
	}
}

// Two consumers constructed in the same process must never share a group
// id — the whole correctness argument for a full per-instance replay rests
// on this.
func TestUniqueConsumerGroupDiffersPerCall(t *testing.T) {
	a, b := uniqueConsumerGroup(), uniqueConsumerGroup()
	if a == b {
		t.Fatalf("expected unique consumer groups, got %q twice", a)
	}
	for _, g := range []string{a, b} {
		if len(g) <= len(consumerGroupPrefix) || g[:len(consumerGroupPrefix)] != consumerGroupPrefix {
			t.Fatalf("group %q does not carry the %q prefix", g, consumerGroupPrefix)
		}
	}
}

func TestCloseReleasesTheReader(t *testing.T) {
	c := newTestConsumer(nil, targetOffsets{})
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// WaitReady must unblock via the readiness channel, not only via the
// already-ready fast path.
func TestWaitReadyUnblocksWhenReadinessArrives(t *testing.T) {
	c := newTestConsumer(nil, targetOffsets{0: 1})

	go func() { c.observe(0, 0) }()

	if err := c.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if !c.Ready() {
		t.Fatal("expected the consumer to be ready after WaitReady returned")
	}
}

// markReady must be safe to call more than once — observe() can reach the
// done condition concurrently on several partitions.
func TestMarkReadyIsIdempotent(t *testing.T) {
	c := newTestConsumer(nil, targetOffsets{})
	c.markReady()
	c.markReady()

	if !c.Ready() {
		t.Fatal("expected ready")
	}
}

func TestNewTargetOffsetsRejectsAnEmptyBrokerList(t *testing.T) {
	if _, err := newTargetOffsets(context.Background(), nil, Topic); err == nil {
		t.Fatal("expected an error with no brokers configured")
	}
}

func TestNewConsumerRejectsAnEmptyBrokerList(t *testing.T) {
	if _, err := NewConsumer(context.Background(), nil, discardLogger()); err == nil {
		t.Fatal("expected an error with no brokers configured")
	}
}

// A decommission for a slot that was never registered must be a harmless
// no-op, not an error or a panic.
func TestDecommissionOfAnUnknownSlotIsANoOp(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "locationslot", "LocationSlotDecommissioned", slotData{
			LocationCode: "WH1-STOR-AMB-Z99-99-99-Z",
		}),
	}, targetOffsets{0: 1})
	drain(t, c)

	if c.Slots() != 0 {
		t.Fatalf("expected 0 cached slots, got %d", c.Slots())
	}
	if !c.Ready() {
		t.Fatal("expected readiness to advance")
	}
}

// A ZoneRegistered with no zoneId is malformed: it must be reported and
// skipped rather than cached under an empty key, where it could later be
// joined to a slot whose code failed to yield a zone id.
func TestZoneRegisteredWithoutZoneIdIsRejected(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "zone", "ZoneRegistered", zoneData{
			ZoneID: "", TemperatureClass: "Ambient",
		}),
	}, targetOffsets{0: 1})
	drain(t, c)

	if c.Zones() != 0 {
		t.Fatalf("expected the malformed zone to be skipped, got %d cached", c.Zones())
	}
}

func TestLocationSlotRegisteredWithoutCodeIsRejected(t *testing.T) {
	c := newTestConsumer([]kafkago.Message{
		envelopeMsg(t, 0, 0, "locationslot", "LocationSlotRegistered", slotData{
			LocationCode: "", ZoneID: "WH1-STOR-AMB",
		}),
	}, targetOffsets{0: 1})
	drain(t, c)

	if c.Slots() != 0 {
		t.Fatalf("expected the malformed slot to be skipped, got %d cached", c.Slots())
	}
}
