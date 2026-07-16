package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// FileOutboundHandler handles xlsx-based batch outbound preview.
type FileOutboundHandler struct {
	stockSvc *service.StockService
	accSvc   *service.AccessoryService
}

func NewFileOutboundHandler(stockSvc *service.StockService, accSvc *service.AccessoryService) *FileOutboundHandler {
	return &FileOutboundHandler{stockSvc: stockSvc, accSvc: accSvc}
}

// FileOutboundPreviewItem is one matched row in the preview response.
type FileOutboundPreviewItem struct {
	AccessoryID       int64  `json:"accessory_id"`
	Name              string `json:"name"`
	Quantity          int64  `json:"quantity"`
	CurrentStock      int64  `json:"current_stock"`
	LowStockThreshold int64  `json:"low_stock_threshold"`
	Stall             string `json:"stall"`
}

// FileOutboundNotFound is one unmatched name from the xlsx.
type FileOutboundNotFound struct {
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
	Stall    string `json:"stall"`
}

// FileOutboundPreview is the response for POST /api/v1/stock/file_outbound.
type FileOutboundPreview struct {
	Items         []FileOutboundPreviewItem `json:"items"`
	NotFound      []FileOutboundNotFound    `json:"not_found"`
	TotalItems    int                       `json:"total_items"`
	MatchedCount  int                       `json:"matched_count"`
	NotFoundCount int                       `json:"not_found_count"`
}

// itemPattern matches "名称 x数量" with optional whitespace around "x".
var itemPattern = regexp.MustCompile(`^(.+)\s+x(\d+)$`)

// Preview — POST /api/v1/stock/file_outbound
// Accepts a multipart form with an "file" field containing the xlsx.
// Parses the "汇总" sheet, matches names against the catalog, and returns
// a preview without executing any stock changes.
func (h *FileOutboundHandler) Preview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	agg, err := parseXlsxSummary(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	// Resolve against the catalog.
	preview := FileOutboundPreview{
		Items:    make([]FileOutboundPreviewItem, 0),
		NotFound: make([]FileOutboundNotFound, 0),
	}
	for _, entry := range agg {
		acc, err := h.accSvc.GetByName(r.Context(), entry.name)
		if err != nil {
			preview.NotFound = append(preview.NotFound, FileOutboundNotFound{
				Name:     entry.name,
				Quantity: entry.qty,
				Stall:    entry.stall,
			})
			continue
		}
		preview.Items = append(preview.Items, FileOutboundPreviewItem{
			AccessoryID:       acc.ID,
			Name:              acc.Name,
			Quantity:          entry.qty,
			CurrentStock:      acc.CurrentStock,
			LowStockThreshold: acc.LowStockThreshold,
			Stall:             entry.stall,
		})
	}

	preview.TotalItems = len(agg)
	preview.MatchedCount = len(preview.Items)
	preview.NotFoundCount = len(preview.NotFound)

	writeJSON(w, http.StatusOK, preview)
}

// Execute — POST /api/v1/stock/file_outbound/execute
// Accepts the same xlsx but also executes the outbound. Missing accessories
// are auto-created; insufficient stock sets current_stock to 0 and bumps
// low_stock_threshold by the shortage.
func (h *FileOutboundHandler) Execute(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	agg, err := parseXlsxSummary(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	items := make([]service.FileOutboundItem, 0, len(agg))
	for _, entry := range agg {
		items = append(items, service.FileOutboundItem{Name: entry.name, Quantity: entry.qty, Stall: entry.stall})
	}

	res, err := h.stockSvc.FileForceOutbound(r.Context(), items)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- xlsx parsing helpers ----------------------------------------------------

// readUploadedXlsxBytes extracts the xlsx payload from a file-upload
// request. It supports two transports:
//
//   - multipart/form-data with a "file" field (the original API shape,
//     still used by tests and any external caller);
//   - a raw request body (Content-Type: application/octet-stream or any
//     non-multipart type), used by the Wails GUI frontend.
//
// The raw-body path exists because Windows WebView2 delivers POST bodies
// to the Wails assetserver with a ContentLength/Content-Length header
// mismatch that corrupts multipart boundaries by the time the request
// reaches the embedded HTTP server via the reverse proxy. Sending the
// xlsx as a plain byte stream sidesteps multipart entirely and is
// reliable across all transports.
func readUploadedXlsxBytes(r *http.Request) ([]byte, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			return nil, fmt.Errorf("failed to parse multipart form: %w", err)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("missing 'file' field in form: %w", err)
		}
		defer file.Close()
		return io.ReadAll(file)
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read uploaded file: %w", err)
	}
	return raw, nil
}

type aggEntry struct {
	name  string
	qty   int64
	stall string
}

// parseXlsxSummary reads the uploaded xlsx and returns aggregated name→qty
// from the "汇总" sheet. Row 0 is the header: each column's header cell
// is the stall (档口) name, and the cells below it are "名称 x数量" entries
// belonging to that stall. Same-name entries across columns are aggregated
// (qty summed); the stall is taken from the first column the name appears
// in. Callers should already have set MaxBytesReader.
func parseXlsxSummary(r *http.Request) ([]aggEntry, error) {
	raw, err := readUploadedXlsxBytes(r)
	if err != nil {
		return nil, err
	}

	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to open xlsx: %w", err)
	}
	defer xf.Close()

	rows, err := xf.GetRows("汇总")
	if err != nil {
		return nil, fmt.Errorf("sheet '汇总' not found: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("汇总 sheet has no data rows (need header + at least 1 data row)")
	}

	// Row 0 = stall names (one per column).
	header := rows[0]
	if len(header) == 0 {
		return nil, fmt.Errorf("汇总 sheet header row is empty")
	}

	aggMap := make(map[string]*aggEntry)
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		for colIdx, cell := range rows[rowIdx] {
			if cell == "" {
				continue
			}
			m := itemPattern.FindStringSubmatch(cell)
			if m == nil {
				continue
			}
			name := m[1]
			var qty int64
			if _, err := fmt.Sscanf(m[2], "%d", &qty); err != nil {
				continue
			}
			// Stall from this column's header; default to "未分配" if
			// the header cell is blank or the column index exceeds the
			// header length.
			stall := "未分配"
			if colIdx < len(header) && strings.TrimSpace(header[colIdx]) != "" {
				stall = strings.TrimSpace(header[colIdx])
			}
			if existing, ok := aggMap[name]; ok {
				existing.qty += qty
			} else {
				aggMap[name] = &aggEntry{name: name, qty: qty, stall: stall}
			}
		}
	}

	out := make([]aggEntry, 0, len(aggMap))
	for _, e := range aggMap {
		out = append(out, *e)
	}
	return out, nil
}
