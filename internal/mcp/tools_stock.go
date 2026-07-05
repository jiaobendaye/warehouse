// Package mcp — stock tools.
//
// stock.inbound and stock.outbound are single-row operations; stock.batch_*
// are atomic multi-row operations (the service wraps them in one tx).
// client_ref provides idempotency: when the same ref is sent twice, the
// second call returns the original flow without touching stock again.
package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// stockInboundInput is the JSON shape for stock.inbound.
type stockInboundInput struct {
	AccessoryID int64   `json:"accessory_id"          jsonschema:"target accessory id"`
	Quantity    int64   `json:"quantity"              jsonschema:"units to add (must be > 0)"`
	UnitCost    float64 `json:"unit_cost,omitempty"   jsonschema:"per-unit cost (informational)"`
	Remark      string  `json:"remark,omitempty"      jsonschema:"free-form remark"`
	OccurredAt  string  `json:"occurred_at,omitempty" jsonschema:"RFC3339 occurrence time (defaults to now)"`
	ClientRef   string  `json:"client_ref,omitempty"  jsonschema:"idempotency key; same key returns the original flow"`
}

// stockOutboundInput mirrors stockInboundInput but exposes unit_price.
type stockOutboundInput struct {
	AccessoryID int64   `json:"accessory_id"          jsonschema:"target accessory id"`
	Quantity    int64   `json:"quantity"              jsonschema:"units to remove (must be > 0 and ≤ stock)"`
	UnitPrice   float64 `json:"unit_price,omitempty"  jsonschema:"per-unit price (informational)"`
	Remark      string  `json:"remark,omitempty"      jsonschema:"free-form remark"`
	OccurredAt  string  `json:"occurred_at,omitempty" jsonschema:"RFC3339 occurrence time (defaults to now)"`
	ClientRef   string  `json:"client_ref,omitempty"  jsonschema:"idempotency key; same key returns the original flow"`
}

// stockBatchInboundInput is the JSON shape for stock.batch_inbound.
type stockBatchInboundInput struct {
	Items []stockInboundInput `json:"items" jsonschema:"list of inbound ops; all-or-nothing transaction"`
}

// stockBatchOutboundInput is the JSON shape for stock.batch_outbound.
type stockBatchOutboundInput struct {
	Items []stockOutboundInput `json:"items" jsonschema:"list of outbound ops; all-or-nothing transaction"`
}

// batchResultOutput mirrors service.BatchResult with a JSON-friendly shape.
type batchResultOutput struct {
	Accepted int                   `json:"accepted"`
	Flows    []domain.InventoryFlow `json:"flows"`
	IDs      []int64               `json:"ids"`
}

func registerStockTools(srv *mcpsdk.Server, svc *service.StockService) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "stock.inbound", Description: "Record a single inbound (stock-in) operation. Idempotent via client_ref.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in stockInboundInput) (*mcpsdk.CallToolResult, domain.InventoryFlow, error) {
		flow, err := svc.Inbound(ctx, service.InboundCmd{
			AccessoryID: in.AccessoryID,
			Quantity:    in.Quantity,
			UnitCost:    in.UnitCost,
			Remark:      in.Remark,
			OccurredAt:  in.OccurredAt,
			ClientRef:   in.ClientRef,
		})
		if err != nil {
			return nil, domain.InventoryFlow{}, rpcError(err)
		}
		return nil, flow, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "stock.outbound", Description: "Record a single outbound (stock-out) operation. Fails with INSUFFICIENT_STOCK if quantity > stock.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in stockOutboundInput) (*mcpsdk.CallToolResult, domain.InventoryFlow, error) {
		flow, err := svc.Outbound(ctx, service.OutboundCmd{
			AccessoryID: in.AccessoryID,
			Quantity:    in.Quantity,
			UnitPrice:   in.UnitPrice,
			Remark:      in.Remark,
			OccurredAt:  in.OccurredAt,
			ClientRef:   in.ClientRef,
		})
		if err != nil {
			return nil, domain.InventoryFlow{}, rpcError(err)
		}
		return nil, flow, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "stock.batch_inbound", Description: "Apply N inbound operations under one transaction (all-or-nothing).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in stockBatchInboundInput) (*mcpsdk.CallToolResult, batchResultOutput, error) {
		items := make([]service.InboundCmd, 0, len(in.Items))
		for _, it := range in.Items {
			items = append(items, service.InboundCmd{
				AccessoryID: it.AccessoryID,
				Quantity:    it.Quantity,
				UnitCost:    it.UnitCost,
				Remark:      it.Remark,
				OccurredAt:  it.OccurredAt,
				ClientRef:   it.ClientRef,
			})
		}
		res, err := svc.BatchInbound(ctx, items)
		if err != nil {
			return nil, batchResultOutput{}, rpcError(err)
		}
		return nil, batchResultOutput{Accepted: res.Accepted, Flows: res.Flows, IDs: res.IDs}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "stock.batch_outbound", Description: "Apply N outbound operations under one transaction (all-or-nothing).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in stockBatchOutboundInput) (*mcpsdk.CallToolResult, batchResultOutput, error) {
		items := make([]service.OutboundCmd, 0, len(in.Items))
		for _, it := range in.Items {
			items = append(items, service.OutboundCmd{
				AccessoryID: it.AccessoryID,
				Quantity:    it.Quantity,
				UnitPrice:   it.UnitPrice,
				Remark:      it.Remark,
				OccurredAt:  it.OccurredAt,
				ClientRef:   it.ClientRef,
			})
		}
		res, err := svc.BatchOutbound(ctx, items)
		if err != nil {
			return nil, batchResultOutput{}, rpcError(err)
		}
		return nil, batchResultOutput{Accepted: res.Accepted, Flows: res.Flows, IDs: res.IDs}, nil
	})
}