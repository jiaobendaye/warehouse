package api

import (
	"encoding/json"
	"net/http"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// ReplenishmentHandler exposes ReplenishmentService over HTTP.
type ReplenishmentHandler struct {
	svc *service.ReplenishmentService
}

func NewReplenishmentHandler(svc *service.ReplenishmentService) *ReplenishmentHandler {
	return &ReplenishmentHandler{svc: svc}
}

// Scan — GET /api/v1/replenishment/scan
func (h *ReplenishmentHandler) Scan(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Scan(r.Context())
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Check — POST /api/v1/replenishment/check  body: { "names": [...], "policy": "..." }
func (h *ReplenishmentHandler) Check(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names  []string `json:"names"`
		Policy string   `json:"policy"`
	}
	if err := decodeJSON(r, &req); err != nil {
		// Be lenient: if the body is a bare JSON array or string, surface a
		// helpful error.
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must be {names:[], policy:\"\"}: "+err.Error())
		return
	}
	res, err := h.svc.Check(r.Context(), req.Names, req.Policy)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// json.RawMessage import guard for future-proof strict-body decoding.
var _ = json.RawMessage(nil)