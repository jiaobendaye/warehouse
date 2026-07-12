// Package mcp — file inbound tools.
//
// stock.file_inbound reads an xlsx from a local file path, parses the
// FIRST sheet (row 0 = header, rows 1..N = [name, qty]), then executes
// a batch inbound. Missing accessories are auto-created with stock=0.
// This is the MCP counterpart of POST /api/v1/stock/file_inbound.
package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// fileInboundInput is the JSON shape for the stock.file_inbound tool.
type fileInboundInput struct {
	FilePath    string `json:"file_path" jsonschema:"absolute path to the xlsx file"`
	Calibration bool   `json:"calibration,omitempty" jsonschema:"when true the second column is the target stock level rather than a delta"`
}

// fileInboundItem is one row in the response.
type fileInboundItem struct {
	Name         string `json:"name"`
	Quantity     int64  `json:"quantity"`
	AccessoryID  int64  `json:"accessory_id"`
	Created      bool   `json:"created"`
	FlowID       int64  `json:"flow_id"`
	BalanceAfter int64  `json:"balance_after"`
}

// fileInboundOutput mirrors the HTTP response shape.
type fileInboundOutput struct {
	Inbound    int               `json:"inbound"`
	Created    int               `json:"created"`
	TotalItems int               `json:"total_items"`
	Items      []fileInboundItem `json:"items"`
}

func registerFileInboundTools(srv *mcpsdk.Server, stockSvc *service.StockService) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "stock.file_inbound",
		Description: "Read an xlsx file (first sheet) where row 0 is a header and rows 1..N are [name, qty] pairs. Auto-creates missing accessories and records an inbound flow for every row in a single transaction. Pass calibration=true to interpret the qty column as the desired absolute stock level (set-to-X semantics) instead of a delta.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in fileInboundInput) (*mcpsdk.CallToolResult, fileInboundOutput, error) {
		entries, err := parseXlsxInboundFile(in.FilePath, in.Calibration)
		if err != nil {
			return nil, fileInboundOutput{}, fmt.Errorf("parse xlsx: %w", err)
		}
		if len(entries) == 0 {
			return nil, fileInboundOutput{}, fmt.Errorf("xlsx has no usable data rows")
		}

		items := make([]service.FileInboundItem, 0, len(entries))
		for _, e := range entries {
			items = append(items, service.FileInboundItem{Name: e.name, Quantity: e.qty, Calibration: in.Calibration})
		}
		res, err := stockSvc.FileInbound(ctx, items)
		if err != nil {
			return nil, fileInboundOutput{}, rpcError(err)
		}

		out := fileInboundOutput{
			Inbound:    res.Inbound,
			Created:    res.Created,
			TotalItems: res.Inbound,
			Items:      make([]fileInboundItem, 0, len(res.Flows)),
		}
		for i, f := range res.Flows {
			row := fileInboundItem{
				Name:         entries[i].name,
				Quantity:     entries[i].qty,
				AccessoryID:  f.AccessoryID,
				FlowID:       f.ID,
				BalanceAfter: f.BalanceAfter,
				Created:      res.CreatedNames[i],
			}
			out.Items = append(out.Items, row)
		}
		return nil, out, nil
	})
}

// fileInboundAggEntry mirrors api.fileInboundEntry — kept as a separate
// type so the MCP package doesn't depend on the api package for parsing.
type fileInboundAggEntry struct {
	name string
	qty  int64
}

// parseXlsxInboundFile is the MCP-side counterpart of api.parseXlsxInbound.
// Both follow the same rules (first sheet, header row 0, [name, qty] data
// rows, trim names, dedup duplicates, skip non-positive/non-numeric qty).
// The duplication is intentional: the MCP layer is an alternate transport
// that may be embedded in agents which don't go through the HTTP handler.
//
// When calibration=true, duplicate names use LAST-wins and qty=0 rows are
// kept (target stock of zero is valid); otherwise the inbound behaviour
// (sum on duplicate names, skip qty<=0) is preserved.
func parseXlsxInboundFile(path string, calibration bool) ([]fileInboundAggEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer xf.Close()

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
	if len(rows[0]) < 2 {
		return nil, fmt.Errorf("header must have at least 2 columns (name, qty), got %d", len(rows[0]))
	}
	if strings.TrimSpace(rows[0][0]) == "" {
		return nil, fmt.Errorf("header column A is empty — expected name column")
	}

	aggMap := make(map[string]*fileInboundAggEntry)
	order := make([]string, 0)
	for r := 1; r < len(rows); r++ {
		row := rows[r]
		if len(row) < 2 {
			continue
		}
		name := strings.TrimSpace(row[0])
		qtyStr := strings.TrimSpace(row[1])
		if name == "" || qtyStr == "" {
			continue
		}
		qty, err := strconv.ParseInt(qtyStr, 10, 64)
		if err != nil || qty < 0 {
			continue
		}
		if !calibration && qty <= 0 {
			continue
		}
		if existing, ok := aggMap[name]; ok {
			if calibration {
				existing.qty = qty // last-wins
			} else {
				existing.qty += qty
			}
		} else {
			aggMap[name] = &fileInboundAggEntry{name: name, qty: qty}
			order = append(order, name)
		}
	}
	out := make([]fileInboundAggEntry, 0, len(order))
	for _, n := range order {
		out = append(out, *aggMap[n])
	}
	return out, nil
}
