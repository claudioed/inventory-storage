package facilitylayout_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/claudioed/inventory-storage/internal/adapters/outbound/facilitylayout"
	"github.com/claudioed/inventory-storage/internal/domain/product"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// fakeDoer is a stub facilitylayout.HTTPDoer so these tests never hit the
// network.
type fakeDoer struct {
	resp *http.Response
	err  error
	req  *http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

// jsonResponse builds a fake *http.Response for fakeDoer. The body is
// always closed by the code under test
// (facilitylayout.Client.GetSlotAttributes defers resp.Body.Close() on
// every return path); bodyclose's static analysis cannot trace that
// through the HTTPDoer interface boundary from this test file, so each
// call site below carries an explicit nolint.
func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClient_GetSlotAttributes_200_Hazmat(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{"hazmat": true, "temperatureClass": "Ambient"}`)} //nolint:bodyclose
	client := facilitylayout.NewClient("http://facility-layout.local", doer)

	attrs, err := client.GetSlotAttributes(context.Background(), shared.BinId("A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !attrs.Known {
		t.Fatalf("expected Known=true")
	}
	if !attrs.Hazmat {
		t.Fatalf("expected Hazmat=true")
	}
	if attrs.TemperatureClass != product.Ambient {
		t.Fatalf("expected TemperatureClass=Ambient, got %v", attrs.TemperatureClass)
	}
	if doer.req.URL.Path != "/locations/A-1-1/classification" {
		t.Fatalf("expected path /locations/A-1-1/classification, got %s", doer.req.URL.Path)
	}
}

func TestClient_GetSlotAttributes_404_FailsOpen(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusNotFound, "")} //nolint:bodyclose
	client := facilitylayout.NewClient("http://facility-layout.local", doer)

	attrs, err := client.GetSlotAttributes(context.Background(), shared.BinId("Z-9-9"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs.Known {
		t.Fatalf("expected Known=false on 404")
	}
}

func TestClient_GetSlotAttributes_500_ReturnsError(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusInternalServerError, "")} //nolint:bodyclose
	client := facilitylayout.NewClient("http://facility-layout.local", doer)

	_, err := client.GetSlotAttributes(context.Background(), shared.BinId("A-1-1"))
	if !errors.Is(err, facilitylayout.ErrUnexpectedStatus) {
		t.Fatalf("expected ErrUnexpectedStatus, got %v", err)
	}
}

func TestClient_GetSlotAttributes_TransportError_Propagates(t *testing.T) {
	transportErr := errors.New("connection refused")
	doer := &fakeDoer{err: transportErr}
	client := facilitylayout.NewClient("http://facility-layout.local", doer)

	_, err := client.GetSlotAttributes(context.Background(), shared.BinId("A-1-1"))
	if !errors.Is(err, transportErr) {
		t.Fatalf("expected transport error to propagate, got %v", err)
	}
}

func TestClient_GetSlotAttributes_MalformedJSON_ReturnsError(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{not-json`)} //nolint:bodyclose
	client := facilitylayout.NewClient("http://facility-layout.local", doer)

	_, err := client.GetSlotAttributes(context.Background(), shared.BinId("A-1-1"))
	if err == nil {
		t.Fatalf("expected an error decoding malformed JSON")
	}
}

// An unparseable temperatureClass value in an otherwise-200 response
// should not fail the whole lookup — the zone may simply carry no
// temperature constraint.
func TestClient_GetSlotAttributes_UnparseableTemperatureClass_FallsBackToZeroValue(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{"hazmat": false, "temperatureClass": ""}`)} //nolint:bodyclose
	client := facilitylayout.NewClient("http://facility-layout.local", doer)

	attrs, err := client.GetSlotAttributes(context.Background(), shared.BinId("A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs.TemperatureClass != "" {
		t.Fatalf("expected empty TemperatureClass, got %v", attrs.TemperatureClass)
	}
}

func TestNewClient_NilDoer_DefaultsToRealHTTPClient(t *testing.T) {
	client := facilitylayout.NewClient("http://facility-layout.local", nil)
	if client == nil {
		t.Fatalf("expected a non-nil client")
	}
}

func TestPermissiveLookup_AlwaysReportsUnknown(t *testing.T) {
	lookup := facilitylayout.NewPermissiveLookup()
	attrs, err := lookup.GetSlotAttributes(context.Background(), shared.BinId("A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs.Known {
		t.Fatalf("expected Known=false from PermissiveLookup")
	}
}

// sanity: confirm we serialize the request the way facility-layout expects
// (Accept header set, GET method).
func TestClient_GetSlotAttributes_SetsAcceptHeaderAndMethod(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{"hazmat": false, "temperatureClass": "Chilled"}`)} //nolint:bodyclose
	client := facilitylayout.NewClient("http://facility-layout.local", doer)

	_, err := client.GetSlotAttributes(context.Background(), shared.BinId("A-1-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.req.Method != http.MethodGet {
		t.Fatalf("expected GET, got %s", doer.req.Method)
	}
	if doer.req.Header.Get("Accept") != "application/json" {
		t.Fatalf("expected Accept: application/json header")
	}
}
