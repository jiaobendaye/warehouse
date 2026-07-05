// Package mcp — file outbound tools.
//
// stock.file_outbound reads an xlsx from a local file path, parses the
// "汇总" sheet, matches names against the catalog, and returns a preview.
// stock.file_outbound.execute does the same but also executes the outbound,
// auto-creating missing accessories and handling insufficient stock.
package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/xuri/excelize/v2"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// itemPattern matches "名称 x数量".
var fileItemPattern = regexp.MustCompile(`^(.+)\s+x(\d+)$`)

// fileOutboundInput is the JSON shape for both tools.
type fileOutboundInput struct {
	FilePath string `json:"file_path" jsonschema:"absolute path to the xlsx file"`
}

// fileOutboundPreviewItem mirrors the HTTP API shape.
type fileOutboundPreviewItem struct {
	AccessoryID  int64  `json:"accessory_id"`
	Name         string `json:"name"`
	Quantity     int64  `json:"quantity"`
	CurrentStock int64  `json:"current_stock"`
}

// fileOutboundNotFoundItem is one unmatched name.
type fileOutboundNotFoundItem struct {
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
}

// fileOutboundPreviewOutput mirrors the HTTP preview response.
type fileOutboundPreviewOutput struct {
	Items         []fileOutboundPreviewItem  `json:"items"`
	NotFound      []fileOutboundNotFoundItem `json:"not_found"`
	TotalItems    int                        `json:"total_items"`
	MatchedCount  int                        `json:"matched_count"`
	NotFoundCount int                        `json:"not_found_count"`
}

// fileOutboundExecuteOutput wraps the execution result.
type fileOutboundExecuteOutput struct {
	Outbound  int   `json:"outbound"`
	Created   int   `json:"created"`
	Shortages int   `json:"shortages"`
	IDs       []int64 `json:"ids"`
}

func registerFileOutboundTools(srv *mcpsdk.Server, stockSvc *service.StockService, accSvc *service.AccessoryService) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "stock.file_outbound",
		Description: "Parse an xlsx shipment file (汇总 sheet) and return a preview of matched accessories and not-found names. Does NOT execute any stock changes.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in fileOutboundInput) (*mcpsdk.CallToolResult, fileOutboundPreviewOutput, error) {
		agg, err := parseXlsxFile(in.FilePath)
		if err != nil {
			return nil, fileOutboundPreviewOutput{}, fmt.Errorf("parse xlsx: %w", err)
		}

		out := fileOutboundPreviewOutput{
			Items:    make([]fileOutboundPreviewItem, 0),
			NotFound: make([]fileOutboundNotFoundItem, 0),
		}
		for _, entry := range agg {
			acc, err := accSvc.GetByName(ctx, entry.name)
			if err != nil {
				out.NotFound = append(out.NotFound, fileOutboundNotFoundItem{
					Name: entry.name, Quantity: entry.qty,
				})
				continue
			}
			out.Items = append(out.Items, fileOutboundPreviewItem{
				AccessoryID: acc.ID, Name: acc.Name,
				Quantity: entry.qty, CurrentStock: acc.CurrentStock,
			})
		}
		out.TotalItems = len(agg)
		out.MatchedCount = len(out.Items)
		out.NotFoundCount = len(out.NotFound)
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "stock.file_outbound.execute",
		Description: "Parse an xlsx shipment file and EXECUTE the outbound. Missing accessories are auto-created; insufficient stock sets current_stock to 0 and bumps low_stock_threshold by the shortage.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in fileOutboundInput) (*mcpsdk.CallToolResult, fileOutboundExecuteOutput, error) {
		agg, err := parseXlsxFile(in.FilePath)
		if err != nil {
			return nil, fileOutboundExecuteOutput{}, fmt.Errorf("parse xlsx: %w", err)
		}

		items := make([]service.FileOutboundItem, 0, len(agg))
		for _, entry := range agg {
			items = append(items, service.FileOutboundItem{Name: entry.name, Quantity: entry.qty})
		}

		res, err := stockSvc.FileForceOutbound(ctx, items)
		if err != nil {
			return nil, fileOutboundExecuteOutput{}, rpcError(err)
		}
		return nil, fileOutboundExecuteOutput{
			Outbound:  res.Outbound,
			Created:   res.Created,
			Shortages: res.Shortages,
			IDs:       res.IDs,
		}, nil
	})
}

type aggEntry struct {
	name string
	qty  int64
}

// parseXlsxFile reads an xlsx from disk and aggregates name→qty from the
// "汇总" sheet.
func parseXlsxFile(path string) ([]aggEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer xf.Close()

	rows, err := xf.GetRows("汇总")
	if err != nil {
		return nil, fmt.Errorf("sheet '汇总': %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("汇总 sheet has no data rows")
	}

	aggMap := make(map[string]*aggEntry)
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		for _, cell := range rows[rowIdx] {
			if cell == "" {
				continue
			}
			m := fileItemPattern.FindStringSubmatch(cell)
			if m == nil {
				continue
			}
			name := m[1]
			var qty int64
			if _, err := fmt.Sscanf(m[2], "%d", &qty); err != nil {
				continue
			}
			if existing, ok := aggMap[name]; ok {
				existing.qty += qty
			} else {
				aggMap[name] = &aggEntry{name: name, qty: qty}
			}
		}
	}
	out := make([]aggEntry, 0, len(aggMap))
	for _, e := range aggMap {
		out = append(out, *e)
	}
	return out, nil
}