package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"

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

// Export — GET /api/v1/replenishment/export
//
// Streams the same set of rows Scan would return as an .xlsx file, sorted
// by shortage DESC so the most urgent items are at the top — matching the
// UI ordering in Replenishment.tsx. The endpoint exists so the user can
// hand the report to a buyer without copying from the screen.
//
// Columns mirror the scan result fields, in the order the UI displays
// them: 名称 / 当前库存 / 阈值 / 缺货量 / 建议补货. Sheet name "告急补货"
// matches the other human-facing sheets ("配件库存" from the inventory
// export) so the convention is uniform.
//
// Like the accessory export, the filename embeds the export timestamp in
// local time so multiple downloads in the same session don't collide, and
// errors before the xlsx is built go through WriteError so callers see
// the same {"error":{...}} envelope as the rest of the API.
func (h *ReplenishmentHandler) Export(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Scan(r.Context())
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}

	xlsxBytes, err := buildReplenishmentXLSX(items)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	filename := fmt.Sprintf("replenishment_%s.xlsx", time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(xlsxBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xlsxBytes)
}

// replenishmentExportHeaders is the single source of truth for the column
// order in the exported xlsx. Kept in one place so tests can assert
// against the same strings the handler writes.
var replenishmentExportHeaders = []string{
	"名称",
	"当前库存",
	"阈值",
	"缺货量",
	"建议补货",
}

// buildReplenishmentXLSX writes the scan results to an in-memory xlsx and
// returns the encoded bytes. Sorts by shortage DESC so the most urgent
// rows come first, matching the on-screen table.
func buildReplenishmentXLSX(items []service.ReplenishmentItem) ([]byte, error) {
	// Sort a copy so we don't mutate the slice the caller still holds.
	sorted := make([]service.ReplenishmentItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Shortage != sorted[j].Shortage {
			return sorted[i].Shortage > sorted[j].Shortage
		}
		return sorted[i].Name < sorted[j].Name
	})

	xf := excelize.NewFile()
	defer xf.Close()

	sheet := "告急补货"
	if _, err := xf.NewSheet(sheet); err != nil {
		return nil, fmt.Errorf("new sheet: %w", err)
	}
	if err := xf.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("delete Sheet1: %w", err)
	}

	for c, h := range replenishmentExportHeaders {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		if err := xf.SetCellValue(sheet, cell, h); err != nil {
			return nil, fmt.Errorf("set header %s: %w", cell, err)
		}
	}
	for i, item := range sorted {
		row := i + 2
		values := []any{
			item.Name,
			item.CurrentStock,
			item.Threshold,
			item.Shortage,
			item.SuggestedQuantity,
		}
		for c, v := range values {
			cell, _ := excelize.CoordinatesToCellName(c+1, row)
			if err := xf.SetCellValue(sheet, cell, v); err != nil {
				return nil, fmt.Errorf("set row %d cell %s: %w", row, cell, err)
			}
		}
	}

	var buf bytes.Buffer
	if err := xf.Write(&buf); err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}
	return buf.Bytes(), nil
}

// json.RawMessage import guard for future-proof strict-body decoding.
var _ = json.RawMessage(nil)