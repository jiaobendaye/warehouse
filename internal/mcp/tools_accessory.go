// Package mcp — accessory tools.
//
// These wrap service.AccessoryService for the MCP transport. The tool's
// JSON schema is inferred from the input struct by the SDK; the handler
// decodes args via the SDK (which validates against the schema) and then
// delegates to the service. Errors are translated to JSON-RPC error codes
// per the contract in TranslateError.
//
// accessory.export writes the whole catalog to an .xlsx file on disk
// (under Services.ExportsDir) and returns the absolute path, size,
// sha256, and row count in the structured output. The agent can then
// either read the file directly from the path or hit the existing HTTP
// /api/v1/accessories/export endpoint for a fresh download. We
// deliberately do NOT inline the xlsx bytes (base64 or EmbeddedResource):
// streamable HTTP is the wrong channel for binary payloads, and the
// HTTP endpoint is the natural place for that.
package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xuri/excelize/v2"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// accessoryListInput is the JSON shape for accessory.list.
type accessoryListInput struct {
	Q      string `json:"q,omitempty"      jsonschema:"search query (substring match on name)"`
	Stall  string `json:"stall,omitempty"  jsonschema:"filter by exact stall name; empty = no filter"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size; 0 means default"`
	Offset int    `json:"offset,omitempty" jsonschema:"page offset"`
}

// accessoryListOutput mirrors service.List's return triple.
type accessoryListOutput struct {
	Items []domain.Accessory `json:"items"`
	Total int                `json:"total"`
}

// accessoryListStallsOutput mirrors the REST /api/v1/accessories/stalls
// response shape: a list of distinct stall values currently in use.
type accessoryListStallsOutput struct {
	Stalls []string `json:"stalls" jsonschema:"Distinct stall (档口) values currently in use, sorted alphabetically (case-insensitive)"`
}

// accessoryGetInput — exactly one of id or name must be set.
type accessoryGetInput struct {
	ID   *int64  `json:"id,omitempty"   jsonschema:"accessory id (mutually exclusive with name)"`
	Name *string `json:"name,omitempty" jsonschema:"accessory name (mutually exclusive with id)"`
}

// accessoryCreateInput — required fields per domain.Accessory.Validate.
type accessoryCreateInput struct {
	Name              string `json:"name"                jsonschema:"unique display name"`
	LowStockThreshold int64  `json:"low_stock_threshold" jsonschema:"threshold for replenishment alerts (0 disables)"`
	Notes             string `json:"notes,omitempty"     jsonschema:"free-form notes"`
}

// accessoryUpdateInput — all fields optional except id.
type accessoryUpdateInput struct {
	ID                int64   `json:"id"                            jsonschema:"accessory id"`
	Name              *string `json:"name,omitempty"                jsonschema:"new name"`
	LowStockThreshold *int64  `json:"low_stock_threshold,omitempty" jsonschema:"new threshold"`
	Notes             *string `json:"notes,omitempty"               jsonschema:"new notes"`
}

// accessoryDeleteInput — required id.
type accessoryDeleteInput struct {
	ID int64 `json:"id" jsonschema:"accessory id"`
}

// accessoryDeleteOutput is the small success envelope for delete.
type accessoryDeleteOutput struct {
	Deleted int64 `json:"deleted"`
}

// accessoryExportInput is empty; the tool always exports the whole
// catalog, mirroring the HTTP /api/v1/accessories/export endpoint
// (no q filter, no pagination).
type accessoryExportInput struct{}

// accessoryExportOutput describes the .xlsx written by the
// accessory.export tool. URL is the absolute download URL of the exact
// file we just wrote — the agent can fetch the bytes regardless of
// whether it shares a filesystem with the server. SHA256 lets the agent
// verify integrity after fetching. RowCount counts data rows only
// (header excluded).
type accessoryExportOutput struct {
	Filename string `json:"filename" jsonschema:"basename of the xlsx file (e.g. accessories_20260706_153045.xlsx)"`
	URL      string `json:"url"      jsonschema:"absolute HTTP(S) URL to GET the exact bytes written (no re-generation); safe to fetch with WebFetch/curl"`
	RowCount int    `json:"row_count" jsonschema:"number of data rows in the xlsx (header excluded)"`
	Size     int64  `json:"size"     jsonschema:"file size in bytes"`
	SHA256   string `json:"sha256"   jsonschema:"lowercase hex sha256 of the file bytes for integrity verification after fetching"`
}

// accessoriesExportHeaders mirrors the column order in the exported
// xlsx. Kept in one place so tests can assert against the same strings
// the build writes.
var accessoriesExportHeaders = []string{
	"名称",
	"当前库存",
	"低库存阈值",
	"备注",
	"创建时间",
	"更新时间",
}

func registerAccessoryTools(srv *mcpsdk.Server, svc *service.AccessoryService, exportsDir, publicBaseURL string) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.list", Description: "List accessories (supports keyword q, stall filter, paginated).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryListInput) (*mcpsdk.CallToolResult, accessoryListOutput, error) {
		rows, total, err := svc.List(ctx, in.Q, in.Stall, in.Limit, in.Offset)
		if err != nil {
			return nil, accessoryListOutput{}, rpcError(err)
		}
		return nil, accessoryListOutput{Items: rows, Total: total}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "accessory.list_stalls",
		Description: "List all distinct stall (档口) values currently in use. Mirrors GET /api/v1/accessories/stalls.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, accessoryListStallsOutput, error) {
		stalls, err := svc.ListStalls(ctx)
		if err != nil {
			return nil, accessoryListStallsOutput{}, rpcError(err)
		}
		return nil, accessoryListStallsOutput{Stalls: stalls}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.get", Description: "Get one accessory by id or name (exactly one required).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryGetInput) (*mcpsdk.CallToolResult, domain.Accessory, error) {
		hasID := in.ID != nil
		hasName := in.Name != nil
		if hasID == hasName {
			// both or neither → JSON-RPC -32602 invalid params.
			return nil, domain.Accessory{}, &jsonrpc.Error{
				Code:    CodeInvalidParams,
				Message: "accessory.get requires exactly one of id or name",
			}
		}
		var (
			acc domain.Accessory
			err error
		)
		if hasID {
			acc, err = svc.Get(ctx, *in.ID)
		} else {
			acc, err = svc.GetByName(ctx, *in.Name)
		}
		if err != nil {
			return nil, domain.Accessory{}, rpcError(err)
		}
		return nil, acc, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.create", Description: "Create a new accessory. Name must be unique.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryCreateInput) (*mcpsdk.CallToolResult, domain.Accessory, error) {
		acc, err := svc.Create(ctx, domain.Accessory{
			Name:              in.Name,
			LowStockThreshold: in.LowStockThreshold,
			Notes:             in.Notes,
		})
		if err != nil {
			return nil, domain.Accessory{}, rpcError(err)
		}
		return nil, acc, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.update", Description: "Update an accessory. All fields are partial-update.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryUpdateInput) (*mcpsdk.CallToolResult, domain.Accessory, error) {
		upd := domain.AccessoryUpdate{
			Name:              in.Name,
			LowStockThreshold: in.LowStockThreshold,
			Notes:             in.Notes,
		}
		acc, err := svc.Update(ctx, in.ID, upd)
		if err != nil {
			return nil, domain.Accessory{}, rpcError(err)
		}
		return nil, acc, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.delete", Description: "Delete an accessory. Fails with CONFLICT if any inventory flow references it.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryDeleteInput) (*mcpsdk.CallToolResult, accessoryDeleteOutput, error) {
		if err := svc.Delete(ctx, in.ID); err != nil {
			return nil, accessoryDeleteOutput{}, rpcError(err)
		}
		return nil, accessoryDeleteOutput{Deleted: in.ID}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "accessory.export",
		Description: "Export the entire accessory catalog to an .xlsx file. Returns the absolute download URL, size, sha256, and row_count in the structured output; the URL is the exact bytes written (no re-generation).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ accessoryExportInput) (*mcpsdk.CallToolResult, accessoryExportOutput, error) {
		// Ask for a million rows so the service's internal upper-bound
		// takes effect rather than the default page size. Same trick the
		// HTTP handler uses to signal "give me everything" without
		// adding a new service method.
		const unlimitedPage = 1_000_000
		rows, _, err := svc.List(ctx, "", "", unlimitedPage, 0)
		if err != nil {
			return nil, accessoryExportOutput{}, rpcError(err)
		}

		xlsxBytes, err := buildAccessoriesExportXLSX(rows)
		if err != nil {
			return nil, accessoryExportOutput{}, fmt.Errorf("build xlsx: %w", err)
		}

		filename := fmt.Sprintf("accessories_%s.xlsx", time.Now().Format("20060102_150405"))

		// Write to ExportsDir. mkdir + write live in one place so a
		// missing directory is a single error path. 0o755 / 0o644 match
		// the conventions used by the rest of the repo (data/ is
		// created with the same mode by main.go).
		if err := os.MkdirAll(exportsDir, 0o755); err != nil {
			return nil, accessoryExportOutput{}, fmt.Errorf("mkdir exports dir: %w", err)
		}
		path := filepath.Join(exportsDir, filename)
		if err := os.WriteFile(path, xlsxBytes, 0o644); err != nil {
			return nil, accessoryExportOutput{}, fmt.Errorf("write xlsx: %w", err)
		}

		sum := sha256.Sum256(xlsxBytes)
		sha := hex.EncodeToString(sum[:])

		// Build the absolute download URL the agent should hit. We
		// rely on PublicBaseURL being set by main.go to "<scheme>://
		// <host>:<port>"; if it's empty the file is still on disk but
		// the agent gets no usable URL — return an error so the
		// misconfiguration surfaces immediately instead of silently
		// breaking the workflow.
		if publicBaseURL == "" {
			return nil, accessoryExportOutput{},
				fmt.Errorf("public base URL is empty; configure Services.PublicBaseURL")
		}
		url := strings.TrimRight(publicBaseURL, "/") + "/api/v1/exports/" + filename

		// One TextContent line for human-friendly logs, plus the file
		// metadata in structured JSON so the agent can pick the URL
		// up programmatically. TextContent spells out the URL so an
		// agent that doesn't introspect structured fields knows where
		// to fetch.
		text := fmt.Sprintf(
			"Exported %d accessor(ies) to %s (%d bytes, sha256=%s). "+
				"GET %s to download the exact bytes written; sha256 should match.",
			len(rows), filename, len(xlsxBytes), sha, url,
		)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: text},
			},
			StructuredContent: map[string]any{
				"filename":  filename,
				"url":       url,
				"row_count": len(rows),
				"size":      int64(len(xlsxBytes)),
				"sha256":    sha,
			},
		}, accessoryExportOutput{
			Filename: filename,
			URL:      url,
			RowCount: len(rows),
			Size:     int64(len(xlsxBytes)),
			SHA256:   sha,
		}, nil
	})
}

// buildAccessoriesExportXLSX writes the catalog to an in-memory xlsx
// and returns the encoded bytes. Sheet name is "配件库存" — matches the
// HTTP export endpoint's sheet name and the inbound convention of
// using Chinese sheet names for human-facing spreadsheets.
//
// The build logic mirrors the api handler's buildAccessoriesXLSX. It
// is duplicated here (rather than imported across package boundaries)
// because the xlsx shape is small and the api package depends on
// internal types the mcp package shouldn't have to reach for.
func buildAccessoriesExportXLSX(rows []domain.Accessory) ([]byte, error) {
	xf := excelize.NewFile()
	defer xf.Close()

	sheet := "配件库存"
	if _, err := xf.NewSheet(sheet); err != nil {
		return nil, fmt.Errorf("new sheet: %w", err)
	}
	// excelize creates a default Sheet1; drop it so the file has only
	// our sheet and isn't littered with an empty placeholder.
	if err := xf.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("delete Sheet1: %w", err)
	}

	for c, h := range accessoriesExportHeaders {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		if err := xf.SetCellValue(sheet, cell, h); err != nil {
			return nil, fmt.Errorf("set header %s: %w", cell, err)
		}
	}
	// excelize coords are 1-indexed; row 1 is the header, so the first
	// data row is row 2.
	for i, a := range rows {
		row := i + 2
		values := []any{
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
