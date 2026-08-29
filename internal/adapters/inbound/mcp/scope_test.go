package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScopeGating_DeniesWithoutReadScope proves that a context carrying no
// (or insufficient) scope is rejected by the guard predicate the tool wrapper
// uses. The transport test always presents a valid key, so this white-box
// test is what exercises the denial branch directly.
func TestScopeGating_DeniesWithoutReadScope(t *testing.T) {
	unauth := context.Background()

	t.Run("empty-scope context denied", func(t *testing.T) {
		if scopeAllows(scopeFromContext(unauth), ScopeRead) {
			t.Fatal("empty-scope context must not satisfy ScopeRead")
		}
	})

	t.Run("read scope satisfies read", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), scopeKey{}, ScopeRead)
		if !scopeAllows(scopeFromContext(ctx), ScopeRead) {
			t.Fatal("read scope must satisfy ScopeRead")
		}
	})

	t.Run("read scope does not satisfy read-write", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), scopeKey{}, ScopeRead)
		if scopeAllows(scopeFromContext(ctx), ScopeReadWrite) {
			t.Fatal("read scope must not satisfy ScopeReadWrite")
		}
	})
}

// TestResourceReadDeniedWithoutScope drives a resource read through a server
// whose request context lacks a scope, asserting the handler denies it. It
// uses the SDK's in-memory transport to invoke the real registered handler
// (no HTTP, so no auth middleware runs and the context carries no scope).
func TestResourceReadDeniedWithoutScope(t *testing.T) {
	h := newHarness(t)
	server := NewServer(h.deps)

	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	_, err = cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "inventory://SKU-A/usable"})
	if err == nil {
		t.Fatal("resource read without scope must be denied")
	}
}

// TestParseUsableURI covers the URI-parsing branches of the usable resource,
// which model/client input can drive into every error path.
func TestParseUsableURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantSKU string
		wantErr bool
	}{
		{"valid", "inventory://SKU-A/usable", "SKU-A", false},
		{"missing suffix", "inventory://SKU-A", "", true},
		{"wrong scheme", "queue://SKU-A/usable", "", true},
		{"empty sku", "inventory:///usable", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sku, err := parseUsableURI(tc.uri)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseUsableURI(%q) = %q, want error", tc.uri, sku)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sku != tc.wantSKU {
				t.Fatalf("parseUsableURI(%q) = %q, want %q", tc.uri, sku, tc.wantSKU)
			}
		})
	}
}
