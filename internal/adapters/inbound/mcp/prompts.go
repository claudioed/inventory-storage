package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// triageLowStockSOP is the operational standard-operating-procedure the
// triage_low_stock prompt hands to the model. Per the charter, prompts encode
// how to interpret results, when to escalate, and what "done" means — they
// standardise agent behaviour across clients rather than leaving procedure
// implicit.
const triageLowStockSOP = `You are triaging low or blocked usable inventory in the inventory-storage context. Use only the MCP tools; never assume stock levels — usable, not on-hand, is what constrains a release.

Procedure:
1. For each SKU under suspicion, call check_availability. The usable quantity is on-hand across all bins minus active reservations and held/unlocated stock. A low usable figure despite known receipts means quantity is bound up in reservations, held, or unlocated.
2. When usable is unexpectedly low, inspect the SKU's bins with get_bin_occupancy: compare reserved vs usable per StockUnit and note any unit in an UNLOCATED state (physically present but lost to the system) — that stock is real but currently unusable.
3. If a reservation is stranding usable stock that will not be picked (a failed physical delivery: blocked pod, lost tote, chute jam, short pick), revoke it with revoke_reservation. Revocation is revocable by design — it returns the bound quantity to usable so the order can be re-allocated against a different holding. Only revoke a reservation you have positively identified as failed; do not revoke live demand.

Interpretation:
- Low usable with high reserved is a reservation problem: demand is bound but not flowing to pick.
- Low usable with UNLOCATED units is a reliability problem: stock exists but the system cannot locate it; a cycle count is needed and this is not fixable by revoking reservations.

Escalate to a human when: usable stays low after revoking clearly-failed reservations, the same UNLOCATED units keep appearing (a recurring loss), or you are unsure whether a reservation represents live demand.

Done means: for each SKU you have reported its usable quantity, the bin-level reason for any shortfall (reserved vs unlocated), and either the reservations you revoked (with why each was a confirmed failure) or an explicit escalation. Revoking a reservation is the only state change permitted here; never revoke live demand.`

// registerPrompts adds the workflow prompts (operational SOPs).
func (d Deps) registerPrompts(server *mcp.Server, _ func(context.Context) Scope) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "triage_low_stock",
		Description: "Standard operating procedure for triaging low or blocked usable inventory using the inventory-storage MCP tools.",
	}, func(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "How to triage low usable inventory: read availability, inspect bin occupancy, revoke only confirmed-failed reservations, and know when to escalate.",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: triageLowStockSOP},
			}},
		}, nil
	})
}
