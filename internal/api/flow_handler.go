package api

import (
	"net/http"
	"strconv"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// FlowHandler exposes FlowService over HTTP.
type FlowHandler struct {
	svc *service.FlowService
}

func NewFlowHandler(svc *service.FlowService) *FlowHandler {
	return &FlowHandler{svc: svc}
}

// List — GET /api/v1/flows?accessory_id=&type=&from=&to=&limit=&offset=
func (h *FlowHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accessoryID, _ := strconv.ParseInt(q.Get("accessory_id"), 10, 64)
	typ := q.Get("type")
	from := q.Get("from")
	to := q.Get("to")
	limit := parseIntQuery(r, "limit", 0)
	offset := parseIntQuery(r, "offset", 0)

	var (
		rows  []flowRow
		total int
		err   error
	)
	if accessoryID > 0 {
		rawRows, totalR, e := h.svc.ListByAccessory(r.Context(), accessoryID, typ, from, to, limit, offset)
		rows, total, err = castFlows(rawRows), totalR, e
	} else {
		rawRows, totalR, e := h.svc.List(r.Context(), typ, from, to, limit, offset)
		rows, total, err = castFlows(rawRows), totalR, e
	}
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  rows,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// Get — GET /api/v1/flows/{id}
func (h *FlowHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	fl, err := h.svc.Get(r.Context(), id)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fl)
}

// flowRow is a local alias so the handler can convert []domain.InventoryFlow
// (a strongly-typed slice) into a generic JSON array via reflection. Defining
// it here avoids leaking domain types into the handler's type parameters.
type flowRow = struct {
	ID           int64   `json:"id"`
	AccessoryID  int64   `json:"accessory_id"`
	Type         string  `json:"type"`
	Quantity     int64   `json:"quantity"`
	UnitCost     float64 `json:"unit_cost"`
	UnitPrice    float64 `json:"unit_price"`
	BalanceAfter int64   `json:"balance_after"`
	ClientRef    string  `json:"client_ref,omitempty"`
	Remark       string  `json:"remark,omitempty"`
	OccurredAt   string  `json:"occurred_at"`
	CreatedAt    string  `json:"created_at"`
}

func castFlows(in []domain.InventoryFlow) []flowRow {
	out := make([]flowRow, 0, len(in))
	for _, f := range in {
		out = append(out, flowRow{
			ID:           f.ID,
			AccessoryID:  f.AccessoryID,
			Type:         string(f.Type),
			Quantity:     f.Quantity,
			UnitCost:     f.UnitCost,
			UnitPrice:    f.UnitPrice,
			BalanceAfter: f.BalanceAfter,
			ClientRef:    f.ClientRef,
			Remark:       f.Remark,
			OccurredAt:   f.OccurredAt,
			CreatedAt:    f.CreatedAt,
		})
	}
	return out
}
