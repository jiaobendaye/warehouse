package desktop

import "github.com/jiaobendaye/warehouse/internal/service"

// Services bundles the four service instances the desktop package needs.
// It mirrors api.Services and mcp.Services so wiring is consistent across
// all entry points.
type Services struct {
	Accessory     *service.AccessoryService
	Stock         *service.StockService
	Flow          *service.FlowService
	Replenishment *service.ReplenishmentService
}
