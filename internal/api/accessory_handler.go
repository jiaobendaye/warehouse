package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xuri/excelize/v2"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// AccessoryHandler exposes AccessoryService over HTTP.
type AccessoryHandler struct {
	svc *service.AccessoryService
}

func NewAccessoryHandler(svc *service.AccessoryService) *AccessoryHandler {
	return &AccessoryHandler{svc: svc}
}

// List — GET /api/v1/accessories?q=&stall=&limit=&offset=
func (h *AccessoryHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	stall := r.URL.Query().Get("stall")
	limit := parseIntQuery(r, "limit", 0)
	offset := parseIntQuery(r, "offset", 0)
	rows, total, err := h.svc.List(r.Context(), q, stall, limit, offset)
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

// Stalls — GET /api/v1/accessories/stalls
// Returns the distinct stall values in use, for the frontend filter
// dropdown and create/edit autocomplete.
func (h *AccessoryHandler) Stalls(w http.ResponseWriter, r *http.Request) {
	stalls, err := h.svc.ListStalls(r.Context())
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stalls": stalls})
}

// Create — POST /api/v1/accessories  body: Accessory (without ID/CreatedAt/UpdatedAt)
func (h *AccessoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in domain.Accessory
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	in.ID = 0 // ignore client-supplied id
	out, err := h.svc.Create(r.Context(), in)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// Get — GET /api/v1/accessories/{id}
func (h *AccessoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	a, err := h.svc.Get(r.Context(), id)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// Update — PATCH /api/v1/accessories/{id}  body: AccessoryUpdate (pointer fields).
func (h *AccessoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	var u domain.AccessoryUpdate
	if err := decodeJSON(r, &u); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	out, err := h.svc.Update(r.Context(), id, u)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Delete — DELETE /api/v1/accessories/{id}
func (h *AccessoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// --- shared helpers ------------------------------------------------------

// parseIDParam extracts a numeric {id} path param. On failure it writes a
// 400 and returns ok=false, so handlers can simply `return` on error.
func parseIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// parseIntQuery reads an integer query parameter. Empty/missing returns def.
// Non-numeric values return def silently — the service clamps limit/offset.
func parseIntQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// decodeJSON decodes the body into v, requiring Content-Type application/json.
// Failure (malformed JSON, wrong content-type, unknown fields when strict)
// yields a clean error.
func decodeJSON(r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !startsWith(ct, "application/json") {
		return errors.New("content-type must be application/json")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// writeJSON marshals v and writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Export — GET /api/v1/accessories/export
//
// Streams a full inventory snapshot as an .xlsx file. Always exports the
// whole catalog (no q filter, no pagination) — pagination is the wrong
// primitive for an offline spreadsheet the user wants to walk through.
//
// Columns mirror what the JSON List endpoint surfaces, in the same order:
// 名称 / 当前库存 / 低库存阈值 / 备注 / 创建时间 / 更新时间. Adding or
// removing an Accessory field should keep these two surfaces in sync —
// the export is meant to be a faithful, durable copy of "what the list
// shows right now."
//
// The filename embeds the export timestamp in local time so multiple
// downloads in the same session don't collide. We use attachment so the
// browser downloads rather than renders the file inline.
//
// Errors before the xlsx is built go through WriteError so they share the
// standard {"error":{...}} envelope. Errors during xlsx build are
// unusual (excelize in-memory, no I/O) and bubble up as a plain 500.
func (h *AccessoryHandler) Export(w http.ResponseWriter, r *http.Request) {
	// Pull the whole catalog. The service clamps limit to a sane upper
	// bound; we ask for a million which is well above any realistic
	// accessory count and signals "give me everything" without a new
	// service method.
	rows, _, err := h.svc.List(r.Context(), "", "", 1_000_000, 0)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}

	xlsxBytes, err := buildAccessoriesXLSX(rows)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	filename := fmt.Sprintf("accessories_%s.xlsx", time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(xlsxBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xlsxBytes)
}

// accessoriesExportHeaders is the single source of truth for the column
// order in the exported xlsx. Kept in one place so the test can assert
// against the same strings the handler writes.
var accessoriesExportHeaders = []string{
	"档口",
	"名称",
	"当前库存",
	"低库存阈值",
	"备注",
	"创建时间",
	"更新时间",
}

// buildAccessoriesXLSX writes the catalog to an in-memory xlsx and
// returns the encoded bytes. Sheet name is "配件库存" — matches the
// inbound convention of using Chinese sheet names for human-facing
// spreadsheets.
func buildAccessoriesXLSX(rows []domain.Accessory) ([]byte, error) {
	xf := excelize.NewFile()
	defer xf.Close()

	sheet := "配件库存"
	if _, err := xf.NewSheet(sheet); err != nil {
		return nil, fmt.Errorf("new sheet: %w", err)
	}
	// excelize creates a default Sheet1; drop it so the file has only our
	// sheet and isn't littered with an empty placeholder.
	if err := xf.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("delete Sheet1: %w", err)
	}

	// Header row.
	for c, h := range accessoriesExportHeaders {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		if err := xf.SetCellValue(sheet, cell, h); err != nil {
			return nil, fmt.Errorf("set header %s: %w", cell, err)
		}
	}
	// Sort by stall ascending then name ascending for stable, grouped output.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Stall != rows[j].Stall {
			return rows[i].Stall < rows[j].Stall
		}
		return rows[i].Name < rows[j].Name
	})

	// Data rows. excelize coords are 1-indexed; row 1 is the header, so
	// the first data row is row 2.
	for i, a := range rows {
		row := i + 2
		values := []any{
			a.Stall,
			a.Name,
			a.CurrentStock,
			a.LowStockThreshold,
			a.Notes,
			a.CreatedAt,
			a.UpdatedAt,
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
