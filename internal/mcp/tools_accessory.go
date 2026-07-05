// Package mcp — accessory tools.
//
// These wrap service.AccessoryService for the MCP transport. The tool's
// JSON schema is inferred from the input struct by the SDK; the handler
// decodes args via the SDK (which validates against the schema) and then
// delegates to the service. Errors are translated to JSON-RPC error codes
// per the contract in TranslateError.
package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// accessoryListInput is the JSON shape for accessory.list.
type accessoryListInput struct {
	Q      string `json:"q,omitempty"      jsonschema:"search query (substring match on sku/name)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size; 0 means default"`
	Offset int    `json:"offset,omitempty" jsonschema:"page offset"`
}

// accessoryListOutput mirrors service.List's return triple.
type accessoryListOutput struct {
	Items []domain.Accessory `json:"items"`
	Total int                `json:"total"`
}

// accessoryGetInput — exactly one of id or sku must be set.
type accessoryGetInput struct {
	ID  *int64  `json:"id,omitempty"  jsonschema:"accessory id (mutually exclusive with sku)"`
	SKU *string `json:"sku,omitempty" jsonschema:"accessory sku (mutually exclusive with id)"`
}

// accessoryCreateInput — required fields per domain.Accessory.Validate.
type accessoryCreateInput struct {
	SKU               string `json:"sku"                 jsonschema:"unique sku"`
	Name              string `json:"name"                jsonschema:"display name"`
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

func registerAccessoryTools(srv *mcpsdk.Server, svc *service.AccessoryService) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.list", Description: "List accessories (supports keyword q, paginated).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryListInput) (*mcpsdk.CallToolResult, accessoryListOutput, error) {
		rows, total, err := svc.List(ctx, in.Q, in.Limit, in.Offset)
		if err != nil {
			return nil, accessoryListOutput{}, rpcError(err)
		}
		return nil, accessoryListOutput{Items: rows, Total: total}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.get", Description: "Get one accessory by id or sku (exactly one required).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryGetInput) (*mcpsdk.CallToolResult, domain.Accessory, error) {
		hasID := in.ID != nil
		hasSKU := in.SKU != nil
		if hasID == hasSKU {
			// both or neither → JSON-RPC -32602 invalid params.
			return nil, domain.Accessory{}, &jsonrpc.Error{
				Code:    CodeInvalidParams,
				Message: "accessory.get requires exactly one of id or sku",
			}
		}
		var (
			acc domain.Accessory
			err error
		)
		if hasID {
			acc, err = svc.Get(ctx, *in.ID)
		} else {
			acc, err = svc.GetBySKU(ctx, *in.SKU)
		}
		if err != nil {
			return nil, domain.Accessory{}, rpcError(err)
		}
		return nil, acc, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "accessory.create", Description: "Create a new accessory. SKU must be unique.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in accessoryCreateInput) (*mcpsdk.CallToolResult, domain.Accessory, error) {
		acc, err := svc.Create(ctx, domain.Accessory{
			SKU:               in.SKU,
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
		Name: "accessory.update", Description: "Update an accessory. SKU is immutable; all other fields are partial-update.",
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
}