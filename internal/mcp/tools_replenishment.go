// Package mcp — replenishment tools.
//
// replenishment.scan returns every accessory below its threshold (threshold
// 0 excluded), sorted by shortage descending. replenishment.check inspects
// a list of names and returns those that need replenishment plus a not_found
// list. Policy "default" (or "") suggests shortage units; "fixed:N" suggests
// N units for every short item.
package mcp

import (
	"context"

	"github.com/jiaobendaye/warehouse/internal/service"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
}