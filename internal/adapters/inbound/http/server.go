package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/claudioed/inventory-storage/internal/application/usecases"
	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
	"github.com/claudioed/inventory-storage/internal/domain/stock"
)

// Server holds every use case the HTTP adapter depends on.
type Server struct {
	ReceiveStock      *usecases.ReceiveStock
	StowStock         *usecases.StowStock
	ReserveStock      *usecases.ReserveStock
	RevokeReservation *usecases.RevokeReservation
	ConfirmPick       *usecases.ConfirmPick
	GetUsable         *usecases.GetUsable
	RunCycleCount     *usecases.RunCycleCount
}

// NewRouter builds the chi router for every endpoint in CLAUDE.md's REST API.
func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.handleHealthz)
	r.Post("/stock/receive", s.handleReceiveStock)
	r.Post("/stock/stow", s.handleStowStock)
	r.Post("/reservations", s.handleReserveStock)
	r.Delete("/reservations/{id}", s.handleRevokeReservation)
	r.Post("/reservations/{id}/confirm-pick", s.handleConfirmPick)
	r.Get("/inventory/{sku}/usable", s.handleGetUsable)
	r.Post("/bins/{binId}/cycle-count", s.handleRunCycleCount)

	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReceiveStock(w http.ResponseWriter, r *http.Request) {
	var req receiveStockRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	sku, err := shared.NewSKU(req.SKU)
	if err != nil {
		writeError(w, err)
		return
	}
	qty, err := shared.NewPositiveQuantity(req.Quantity)
	if err != nil {
		writeError(w, err)
		return
	}

	receipt, err := s.ReceiveStock.Execute(r.Context(), sku, qty)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, stagedReceiptResponse{
		SKU:        receipt.SKU.String(),
		Quantity:   receipt.Quantity.Int(),
		ReceivedAt: receipt.ReceivedAt.Format(timeFormat),
	})
}

func (s *Server) handleStowStock(w http.ResponseWriter, r *http.Request) {
	var req stowStockRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	sku, err := shared.NewSKU(req.SKU)
	if err != nil {
		writeError(w, err)
		return
	}
	qty, err := shared.NewPositiveQuantity(req.Quantity)
	if err != nil {
		writeError(w, err)
		return
	}
	binID, err := shared.NewBinId(req.BinID)
	if err != nil {
		writeError(w, err)
		return
	}

	unit, err := s.StowStock.Execute(r.Context(), sku, qty, binID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, toStockUnitResponse(unit))
}

func (s *Server) handleReserveStock(w http.ResponseWriter, r *http.Request) {
	var req reserveStockRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	sku, err := shared.NewSKU(req.SKU)
	if err != nil {
		writeError(w, err)
		return
	}
	qty, err := shared.NewPositiveQuantity(req.Quantity)
	if err != nil {
		writeError(w, err)
		return
	}

	res, err := s.ReserveStock.Execute(r.Context(), sku, qty, req.DemandRef)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, toReservationResponse(res))
}

func (s *Server) handleRevokeReservation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.RevokeReservation.Execute(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConfirmPick(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.ConfirmPick.Execute(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetUsable(w http.ResponseWriter, r *http.Request) {
	skuParam := chi.URLParam(r, "sku")
	sku, err := shared.NewSKU(skuParam)
	if err != nil {
		writeError(w, err)
		return
	}

	usable, err := s.GetUsable.Execute(r.Context(), sku)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, usableInventoryResponse{SKU: usable.SKU.String(), Usable: usable.Usable.Int()})
}

func (s *Server) handleRunCycleCount(w http.ResponseWriter, r *http.Request) {
	binIDParam := chi.URLParam(r, "binId")
	binID, err := shared.NewBinId(binIDParam)
	if err != nil {
		writeError(w, err)
		return
	}

	var req cycleCountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	countedQty, err := shared.NewQuantity(req.CountedQuantity)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := s.RunCycleCount.Execute(r.Context(), binID, countedQty)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, cycleCountResponse{
		BinID:       result.BinID.String(),
		CountedQty:  result.CountedQty.Int(),
		SystemQty:   result.SystemQty.Int(),
		Discrepancy: result.Discrepancy,
	})
}

const timeFormat = "2006-01-02T15:04:05Z07:00"

func toStockUnitResponse(u *stock.StockUnit) stockUnitResponse {
	return stockUnitResponse{
		ID:       u.ID(),
		SKU:      u.SKU().String(),
		BinID:    u.BinID().String(),
		Quantity: u.Quantity().Int(),
		Reserved: u.Reserved().Int(),
		State:    string(u.State()),
	}
}

func toReservationResponse(res *reservation.Reservation) reservationResponse {
	allocations := make([]allocationResponse, 0, len(res.Allocations()))
	for _, a := range res.Allocations() {
		allocations = append(allocations, allocationResponse{StockUnitID: a.StockUnitID, Quantity: a.Quantity.Int()})
	}
	return reservationResponse{
		ID:          res.ID(),
		SKU:         res.SKU().String(),
		Quantity:    res.Quantity().Int(),
		DemandRef:   res.DemandRef(),
		Status:      string(res.Status()),
		Allocations: allocations,
		ExpiresAt:   res.ExpiresAt().Format(timeFormat),
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, statusFor(err), errorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
