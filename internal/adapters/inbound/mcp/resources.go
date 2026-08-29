package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// usableResourceScheme and usableResourceSuffix bracket the SKU in a
// usable-inventory resource URI: inventory://<sku>/usable.
const (
	usableResourceScheme = "inventory://"
	usableResourceSuffix = "/usable"
)

// registerResources adds the scoped read-model resource. Per the charter,
// resources are bounded-context contracts tied to a decision, not bulk dumps:
// the usable-inventory resource answers "how much of this one SKU is usable?"
// for any SKU via an RFC 6570 URI template, backed by the GetUsable read model.
func (d Deps) registerResources(server *mcp.Server, scopeOf func(context.Context) Scope) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: usableResourceScheme + "{sku}" + usableResourceSuffix,
		Name:        "usable inventory by SKU",
		Description: "Usable quantity for a SKU: on-hand across all bins minus active reservations and held/unlocated stock.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		if !scopeAllows(scopeOf(ctx), ScopeRead) {
			return nil, fmt.Errorf("resource %q requires read scope", req.Params.URI)
		}
		skuValue, err := parseUsableURI(req.Params.URI)
		if err != nil {
			return nil, err
		}
		sku, err := shared.NewSKU(skuValue)
		if err != nil {
			return nil, err
		}
		usable, err := d.GetUsable.Execute(ctx, sku)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(checkAvailabilityOutput{SKU: usable.SKU.String(), Usable: usable.Usable.Int()})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			}},
		}, nil
	})
}

// parseUsableURI extracts the SKU from an inventory://<sku>/usable URI. The
// URI comes from a client and is untrusted, so a URI that does not match the
// template shape is rejected rather than silently mishandled.
func parseUsableURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, usableResourceScheme) || !strings.HasSuffix(uri, usableResourceSuffix) {
		return "", fmt.Errorf("unrecognized resource uri %q: want inventory://<sku>/usable", uri)
	}
	sku := strings.TrimSuffix(strings.TrimPrefix(uri, usableResourceScheme), usableResourceSuffix)
	if sku == "" {
		return "", fmt.Errorf("resource uri %q has an empty sku", uri)
	}
	return sku, nil
}
