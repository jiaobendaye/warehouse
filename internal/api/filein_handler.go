package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// FileInboundHandler handles xlsx-based batch inbound.
//
// The endpoint is a single POST /api/v1/stock/file_inbound that accepts a
// multipart xlsx upload. Unlike file_outbound, there is no preview step
// — inbound is idempotent enough (auto-create with stock=0 + same-name
// sum) that the action is safe to fire immediately. The response tells
// the caller how many accessories were created and how many inbound
// flows were recorded, with per-row details for reconciliation.
type FileInboundHandler struct {
	stockSvc *service.StockService
	accSvc   *service.AccessoryService
}

func NewFileInboundHandler(stockSvc *service.StockService, accSvc *service.AccessoryService) *FileInboundHandler {
	return &FileInboundHandler{stockSvc: stockSvc, accSvc: accSvc}
}

// FileInboundItem is one resolved row in the response.
type FileInboundItem struct {
	Name        string `json:"name"`
	Quantity    int64  `json:"quantity"`
	AccessoryID int64  `json:"accessory_id"`
	Created     bool   `json:"created"`
	FlowID      int64  `json:"flow_id"`
	BalanceAfter int64 `json:"balance_after"`
}

// FileInboundResult is the response for POST /api/v1/stock/file_inbound.
type FileInboundResult struct {
	Inbound       int               `json:"inbound"`
	Created       int               `json:"created"`
	TotalItems    int               `json:"total_items"`
	Items         []FileInboundItem `json:"items"`
}

// Inbound — POST /api/v1/stock/file_inbound
// Multipart form: field "file" containing the xlsx. First sheet is read;
// row 0 is the header, rows 1..N are [name, qty] data. Names are trimmed;
// non-positive or non-numeric qty rows are skipped; duplicate names are
// summed before execution.
//
// Optional form field "calibration" (bool) flips the second column
// from "delta to add" to "target stock to set" — when present and true,
// every row is a calibration, the parser keeps LAST-wins on duplicate
// names (since calibration is a set-to-X op), and the response shape
// stays the same as the inbound case.
func (h *FileInboundHandler) Inbound(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	calibration, entries, err := parseXlsxInboundWithMode(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if len(entries) == 0 {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "xlsx has no usable data rows")
		return
	}

	items := make([]service.FileInboundItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, service.FileInboundItem{Name: e.name, Quantity: e.qty, Calibration: calibration})
	}

	res, err := h.stockSvc.FileInbound(r.Context(), items)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}

	resp := FileInboundResult{
		Inbound:    res.Inbound,
		Created:    res.Created,
		TotalItems: res.Inbound,
		Items:      make([]FileInboundItem, 0, len(res.Flows)),
	}
	for i, f := range res.Flows {
		row := FileInboundItem{
			Name:         entries[i].name,
			Quantity:     entries[i].qty,
			AccessoryID:  f.AccessoryID,
			FlowID:       f.ID,
			BalanceAfter: f.BalanceAfter,
			Created:      res.CreatedNames[i],
		}
		resp.Items = append(resp.Items, row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- xlsx parsing ---------------------------------------------------------

// fileInboundEntry is one [name, qty] row extracted from the xlsx. It
// mirrors the unexported type used by filein_handler_test.go.
type fileInboundEntry struct {
	name string
	qty  int64
}

// parseXlsxInbound reads the uploaded xlsx, validates that row 0 looks
// like a [name, qty] header, and aggregates the data rows by name (qty
// is summed across rows with the same name). Names are trimmed of
// leading/trailing whitespace including tabs and newlines. Rows with a
// non-positive or non-numeric qty, or a blank name after trimming, are
// silently skipped — the file_outbound parser follows the same lenient
// policy, and we want both file-* parsers to behave consistently.
//
// Header detection: row 0's first cell must be non-empty after trim.
// That is enough for the actual 入库.xlsx ("配件", "数量") and tolerates
// the user changing the column header text. Column count > 1 is also
// required so we don't misread a single-column sheet.
func parseXlsxInbound(r *http.Request) ([]fileInboundEntry, error) {
	_, entries, err := parseXlsxInboundWithMode(r)
	return entries, err
}

// parseXlsxInboundWithMode is the form-aware variant: it reads the
// optional "calibration" multipart field and aggregates duplicate names
// according to the mode.
//
//   - calibration=false (default): same-name rows SUM their quantities,
//     matching parseXlsxInbound's historical behaviour for file_inbound.
//   - calibration=true: same-name rows use LAST-wins, since calibration
//     is a set-to-X op — the later row reflects the user's most recent
//     intent. This is the only behavioural split between the two modes;
//     the xlsx-reading and per-row validation are shared.
func parseXlsxInboundWithMode(r *http.Request) (calibration bool, _ []fileInboundEntry, retErr error) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		return false, nil, fmt.Errorf("failed to parse multipart form: %w", err)
	}
	calibration = parseBoolFormField(r, "calibration")

	rawRows, err := readXlsxNameQty(r, calibration)
	if err != nil {
		return calibration, nil, err
	}

	aggMap := make(map[string]*fileInboundEntry)
	order := make([]string, 0) // preserves first-seen order for stable output
	for _, rr := range rawRows {
		if calibration {
			// Last-wins: overwrite the previous entry for this name
			// if one exists. Order is preserved by the first-seen
			// name so the response stays indexable.
			if existing, ok := aggMap[rr.name]; ok {
				existing.qty = rr.qty
				continue
			}
			aggMap[rr.name] = &fileInboundEntry{name: rr.name, qty: rr.qty}
			order = append(order, rr.name)
			continue
		}
		if existing, ok := aggMap[rr.name]; ok {
			existing.qty += rr.qty
			continue
		}
		aggMap[rr.name] = &fileInboundEntry{name: rr.name, qty: rr.qty}
		order = append(order, rr.name)
	}

	out := make([]fileInboundEntry, 0, len(order))
	for _, n := range order {
		out = append(out, *aggMap[n])
	}
	return calibration, out, nil
}

// xlsxNameQtyRow is one trimmed, parsed data row from the inbound xlsx,
// before per-mode aggregation. It mirrors fileInboundEntry so the
// aggregation step is mechanical.
type xlsxNameQtyRow struct {
	name string
	qty  int64
}

// readXlsxNameQty reads the FIRST sheet and returns the trimmed,
// numeric, non-empty data rows. Header / qty-validation rules are shared
// between inbound and calibration modes.
//
// In inbound mode (calibration=false) rows with qty <= 0 are skipped —
// that matches parseXlsxInbound's historical behaviour. In calibration
// mode (calibration=true) qty=0 is allowed (the target stock might be
// zero) but negative rows are still skipped, since a target stock cannot
// be negative.
func readXlsxNameQty(r *http.Request, calibration bool) ([]xlsxNameQtyRow, error) {
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("missing 'file' field in form: %w", err)
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read uploaded file: %w", err)
	}

	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to open xlsx: %w", err)
	}
	defer xf.Close()

	// Read the FIRST sheet (regardless of name). The user explicitly
	// specified the file format is "first sheet only" — unlike
	// file_outbound which targets a named "汇总" sheet.
	sheetName := xf.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("xlsx has no sheets")
	}
	rows, err := xf.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("read first sheet: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("first sheet has no data rows (need header + at least 1 data row)")
	}

	// Header check on row 0. We require (a) a non-empty first cell
	// and (b) at least two columns.
	header := rows[0]
	if len(header) < 2 {
		return nil, fmt.Errorf("first sheet header must have at least 2 columns (name, qty), got %d", len(header))
	}
	if strings.TrimSpace(header[0]) == "" {
		return nil, fmt.Errorf("first sheet header is empty in column A — expected 'name' column")
	}

	out := make([]xlsxNameQtyRow, 0)
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]
		if len(row) < 2 {
			continue
		}
		name := strings.TrimSpace(row[0])
		qtyStr := strings.TrimSpace(row[1])
		if name == "" || qtyStr == "" {
			continue
		}
		// qty may arrive as a string ("5") or, for spreadsheets
		// written by code, an int cell. excelize returns numbers as
		// strings too, but defensively handle both.
		var qty int64
		if n, err := strconv.ParseInt(qtyStr, 10, 64); err == nil {
			qty = n
		} else {
			continue
		}
		// Inbound mode: skip non-positive qty. Calibration mode: only
		// skip negative qty (target stock of zero is a valid setting).
		if calibration {
			if qty < 0 {
				continue
			}
		} else if qty <= 0 {
			continue
		}
		out = append(out, xlsxNameQtyRow{name: name, qty: qty})
	}
	return out, nil
}

// parseBoolFormField reads a multipart form value as a bool. Accepts
// "1"/"true"/"TRUE"/"True" as true; "0"/"false"/empty/missing as false.
// Anything else returns false rather than failing the upload — callers
// that need strict parsing should validate before submitting.
func parseBoolFormField(r *http.Request, name string) bool {
	v := strings.TrimSpace(r.FormValue(name))
	switch v {
	case "1", "true", "TRUE", "True":
		return true
	}
	return false
}
