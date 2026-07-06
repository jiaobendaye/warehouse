// Package mcp — replenishment tools.
//
// replenishment.scan returns every accessory below its threshold (threshold
// 0 excluded), sorted by shortage descending. replenishment.check inspects
// a list of names and returns those that need replenishment plus a not_found
// list. Policy "default" (or "") suggests shortage units; "fixed:N" suggests
// N units for every short item.
//
// replenishment.export streams the same scan result set as an .xlsx file
// so an LLM agent can hand the file directly to a buyer. The bytes ride
// inside an EmbeddedResource (Blob), alongside a small TextContent with
// the row count and filename for human-friendly agent logs.
package mcp

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/xuri/excelize/v2"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// replenishmentScanInput is empty; the tool takes no arguments.
type replenishmentScanInput struct{}

// replenishmentScanOutput wraps the scan result. The SDK requires the
// output schema to have type "object", so a bare slice isn't valid; we
// envelope the items in a struct.
type replenishmentScanOutput struct {
	Items []service.ReplenishmentItem `json:"items"`
}

// replenishmentCheckInput is the JSON shape for replenishment.check.
type replenishmentCheckInput struct {
	Names  []string `json:"names"           jsonschema:"name list to inspect"`
	Policy string   `json:"policy,omitempty" jsonschema:"suggested-quantity policy: 'default' or 'fixed:N'"`
}

// batchCheckOutput mirrors service.BatchCheckResult.
type batchCheckOutput struct {
	Items    []service.ReplenishmentItem `json:"items"`
	NotFound []string                    `json:"not_found"`
}

// replenishmentExportInput is empty; the tool takes no arguments. The
// scan always reflects current stock, so there's nothing for the caller
// to specify.
type replenishmentExportInput struct{}

// replenishmentExportOutput is an empty placeholder. The actual file
// bytes are returned via *CallToolResult.Content as an EmbeddedResource,
// not as structured JSON — the SDK requires a generic Out type for the
// handler signature, so we use an empty struct to satisfy it without
// promising any structured payload.
type replenishmentExportOutput struct{}

// replenishmentExportMIME is the standard xlsx MIME used everywhere we
// serve an xlsx file (HTTP endpoint, MCP tool). Keeping a single
// constant ensures an agent that switches between HTTP and MCP gets the
// same content-type it can match on.
const replenishmentExportMIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// replenishmentExportHeaders mirrors the column order in the exported
// xlsx. Kept in one place so tests can assert against the same strings
// the build writes.
var replenishmentExportHeaders = []string{
	"名称",
	"当前库存",
	"阈值",
	"缺货量",
	"建议补货",
}

func registerReplenishmentTools(srv *mcpsdk.Server, svc *service.ReplenishmentService) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "replenishment.scan", Description: "Scan the whole catalog and return every accessory below its low_stock_threshold.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ replenishmentScanInput) (*mcpsdk.CallToolResult, replenishmentScanOutput, error) {
		items, err := svc.Scan(ctx)
		if err != nil {
			return nil, replenishmentScanOutput{}, rpcError(err)
		}
		return nil, replenishmentScanOutput{Items: items}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "replenishment.check", Description: "Given a list of accessory names, return those needing replenishment (plus any unknown names).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in replenishmentCheckInput) (*mcpsdk.CallToolResult, batchCheckOutput, error) {
		res, err := svc.Check(ctx, in.Names, in.Policy)
		if err != nil {
			return nil, batchCheckOutput{}, rpcError(err)
		}
		return nil, batchCheckOutput{Items: res.Items, NotFound: res.NotFound}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "replenishment.export",
		Description: "Export the current replenishment scan as an .xlsx file. Returns the bytes as an embedded resource alongside the filename and row count.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ replenishmentExportInput) (*mcpsdk.CallToolResult, replenishmentExportOutput, error) {
		items, err := svc.Scan(ctx)
		if err != nil {
			return nil, replenishmentExportOutput{}, rpcError(err)
		}

		xlsxBytes, err := buildReplenishmentExportXLSX(items)
		if err != nil {
			return nil, replenishmentExportOutput{}, fmt.Errorf("build xlsx: %w", err)
		}

		filename := fmt.Sprintf("replenishment_%s.xlsx", time.Now().Format("20060102_150405"))
		uri := "embedded://replenishment/" + filename

		// Two-part response: a TextContent with a one-line summary (so the
		// agent can show "exported 12 rows to file X" without parsing the
		// xlsx), then the file itself as an EmbeddedResource.
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{
					Text: fmt.Sprintf("Exported %d replenishment row(s) to %s", len(items), filename),
				},
				&mcpsdk.EmbeddedResource{
					Resource: &mcpsdk.ResourceContents{
						URI:      uri,
						MIMEType: replenishmentExportMIME,
						Blob:     xlsxBytes,
					},
				},
			},
		}, replenishmentExportOutput{}, nil
	})
}

// buildReplenishmentExportXLSX writes the scan rows to an in-memory xlsx
// and returns the encoded bytes. Sorts by shortage DESC, name ASC so the
// most urgent items are at the top — matching the on-screen UI and the
// HTTP export endpoint.
//
// The build logic mirrors the api handler's buildReplenishmentXLSX. It
// is duplicated here (rather than imported across package boundaries)
// because the xlsx shape is small and the api package depends on
// internal types the mcp package shouldn't have to reach for.
func buildReplenishmentExportXLSX(items []service.ReplenishmentItem) ([]byte, error) {
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