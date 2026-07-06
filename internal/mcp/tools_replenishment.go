// Package mcp — replenishment tools.
//
// replenishment.scan returns every accessory below its threshold (threshold
// 0 excluded), sorted by shortage descending. replenishment.check inspects
// a list of names and returns those that need replenishment plus a not_found
// list. Policy "default" (or "") suggests shortage units; "fixed:N" suggests
// N units for every short item.
//
// replenishment.export writes the current scan result to an .xlsx file
// on disk (under Services.ExportsDir) and returns the absolute path,
// size, sha256, and row count in the structured output. The agent can
// either read the file directly from the path or hit the existing HTTP
// /api/v1/replenishment/export endpoint for a fresh download. We
// deliberately do NOT inline the xlsx bytes (base64 or
// EmbeddedResource): streamable HTTP is the wrong channel for binary
// payloads, and the HTTP endpoint is the natural place for that.
package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// replenishmentExportOutput describes the .xlsx written by the
// replenishment.export tool. URL is the absolute download URL of the
// exact file we just wrote — the agent can fetch the bytes regardless
// of whether it shares a filesystem with the server. SHA256 lets the
// agent verify integrity after fetching. RowCount counts data rows
// only (header excluded).
type replenishmentExportOutput struct {
	Filename string `json:"filename" jsonschema:"basename of the xlsx file (e.g. replenishment_20260706_153045.xlsx)"`
	URL      string `json:"url"      jsonschema:"absolute HTTP(S) URL to GET the exact bytes written (no re-generation); safe to fetch with WebFetch/curl"`
	RowCount int    `json:"row_count" jsonschema:"number of data rows in the xlsx (header excluded)"`
	Size     int64  `json:"size"     jsonschema:"file size in bytes"`
	SHA256   string `json:"sha256"   jsonschema:"lowercase hex sha256 of the file bytes for integrity verification after fetching"`
}

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

func registerReplenishmentTools(srv *mcpsdk.Server, svc *service.ReplenishmentService, exportsDir, publicBaseURL string) {
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
		Description: "Export the current replenishment scan to an .xlsx file. Returns the absolute download URL, size, sha256, and row_count in the structured output; the URL is the exact bytes written (no re-generation).",
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

		// Write to ExportsDir. mkdir + write live in one place so a
		// missing directory is a single error path.
		if err := os.MkdirAll(exportsDir, 0o755); err != nil {
			return nil, replenishmentExportOutput{}, fmt.Errorf("mkdir exports dir: %w", err)
		}
		path := filepath.Join(exportsDir, filename)
		if err := os.WriteFile(path, xlsxBytes, 0o644); err != nil {
			return nil, replenishmentExportOutput{}, fmt.Errorf("write xlsx: %w", err)
		}

		sum := sha256.Sum256(xlsxBytes)
		sha := hex.EncodeToString(sum[:])

		// Build the absolute download URL the agent should hit. We
		// rely on PublicBaseURL being set by main.go to "<scheme>://
		// <host>:<port>"; empty means the wiring is wrong — surface
		// that as an error rather than silently returning an
		// unreachable URL.
		if publicBaseURL == "" {
			return nil, replenishmentExportOutput{},
				fmt.Errorf("public base URL is empty; configure Services.PublicBaseURL")
		}
		url := strings.TrimRight(publicBaseURL, "/") + "/api/v1/exports/" + filename

		// One TextContent line for human-friendly logs, plus the file
		// metadata in structured JSON so the agent can pick the URL
		// up programmatically. TextContent spells out the URL so an
		// agent that doesn't introspect structured fields knows where
		// to fetch.
		text := fmt.Sprintf(
			"Exported %d replenishment row(s) to %s (%d bytes, sha256=%s). "+
				"GET %s to download the exact bytes written; sha256 should match.",
			len(items), filename, len(xlsxBytes), sha, url,
		)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: text},
			},
			StructuredContent: map[string]any{
				"filename":  filename,
				"url":       url,
				"row_count": len(items),
				"size":      int64(len(xlsxBytes)),
				"sha256":    sha,
			},
		}, replenishmentExportOutput{
			Filename: filename,
			URL:      url,
			RowCount: len(items),
			Size:     int64(len(xlsxBytes)),
			SHA256:   sha,
		}, nil
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