package api

import (
	"net/http"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// StockHandler exposes StockService over HTTP.
type StockHandler struct {
	svc *service.StockService
}

func NewStockHandler(svc *service.StockService) *StockHandler {
	return &StockHandler{svc: svc}
}

// Inbound — POST /api/v1/stock/inbound  body: InboundCmd
func (h *StockHandler) Inbound(w http.ResponseWriter, r *http.Request) {
	var in service.InboundCmd
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	out, err := h.svc.Inbound(r.Context(), in)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Outbound — POST /api/v1/stock/outbound  body: OutboundCmd
func (h *StockHandler) Outbound(w http.ResponseWriter, r *http.Request) {
	var in service.OutboundCmd
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	out, err := h.svc.Outbound(r.Context(), in)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// BatchInbound — POST /api/v1/stock/batch_inbound  body: []InboundCmd
func (h *StockHandler) BatchInbound(w http.ResponseWriter, r *http.Request) {
	var items []service.InboundCmd
	if err := decodeJSON(r, &items); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	res, err := h.svc.BatchInbound(r.Context(), items)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// BatchOutbound — POST /api/v1/stock/batch_outbound  body: []OutboundCmd
func (h *StockHandler) BatchOutbound(w http.ResponseWriter, r *http.Request) {
	var items []service.OutboundCmd
	if err := decodeJSON(r, &items); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	res, err := h.svc.BatchOutbound(r.Context(), items)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
