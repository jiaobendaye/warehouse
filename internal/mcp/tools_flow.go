// Package mcp — flow tools (read-only queries over the inventory ledger).
package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// flowListInput is the JSON shape for flow.list. accessory_id, type, from,
// to are all optional filters.
type flowListInput struct {
	AccessoryID int64  `json:"accessory_id,omitempty" jsonschema:"filter by accessory id"`
	Type        string `json:"type,omitempty"         jsonschema:"filter by flow type: in or out"`
	From        string `json:"from,omitempty"         jsonschema:"RFC3339 lower bound on occurred_at"`
	To          string `json:"to,omitempty"           jsonschema:"RFC3339 upper bound on occurred_at"`
	Limit       int    `json:"limit,omitempty"        jsonschema:"page size"`
	Offset      int    `json:"offset,omitempty"       jsonschema:"page offset"`
}

// flowListOutput mirrors FlowService.List[ByAccessory].
type flowListOutput struct {
	Items []domain.InventoryFlow `json:"items"`
	Total int                    `json:"total"`
}

// flowGetInput is the JSON shape for flow.get.
type flowGetInput struct {
	ID int64 `json:"id" jsonschema:"flow id"`
}

func registerFlowTools(srv *mcpsdk.Server, svc *service.FlowService) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "flow.list", Description: "Query inventory flows. Supports filtering by accessory, type and time range.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in flowListInput) (*mcpsdk.CallToolResult, flowListOutput, error) {
		var (
			rows []domain.InventoryFlow
			total int
			err   error
		)
		if in.AccessoryID > 0 {
			rows, total, err = svc.ListByAccessory(ctx, in.AccessoryID, in.Type, in.From, in.To, in.Limit, in.Offset)
		} else {
			rows, total, err = svc.List(ctx, in.Type, in.From, in.To, in.Limit, in.Offset)
		}
		if err != nil {
			return nil, flowListOutput{}, rpcError(err)
		}
		return nil, flowListOutput{Items: rows, Total: total}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "flow.get", Description: "Get one inventory flow by id.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in flowGetInput) (*mcpsdk.CallToolResult, domain.InventoryFlow, error) {
		flow, err := svc.Get(ctx, in.ID)
		if err != nil {
			return nil, domain.InventoryFlow{}, rpcError(err)
		}
		return nil, flow, nil
	})
}