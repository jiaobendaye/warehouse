
package desktop

import (
	"context"
	"log"
	"net"
	"strconv"

	"github.com/jiaobendaye/warehouse/internal/config"
	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// App is the top-level Wails binding struct. The frontend accesses its methods
// via window.go.main.App.*.
type App struct {
	ctx           context.Context
	cfg           config.Config
	srvMgr        *ServerManager
	accessory     *service.AccessoryService
	stock         *service.StockService
	flow          *service.FlowService
	replenishment *service.ReplenishmentService
}

// NewApp creates the App struct that will be registered as a Wails binding.
func NewApp(srvMgr *ServerManager, svcs Services) *App {
	return &App{
		srvMgr:        srvMgr,
		accessory:     svcs.Accessory,
		stock:         svcs.Stock,
		flow:          svcs.Flow,
		replenishment: svcs.Replenishment,
	}
}

// SetContext stores the Wails context for use by event handlers.
func (a *App) SetContext(ctx context.Context) { a.ctx = ctx }

// SetConfig stores the parsed configuration.
func (a *App) SetConfig(cfg config.Config) { a.cfg = cfg }

// OnStartup is called by Wails after the window is created. Starts the
// embedded HTTP server (REST + MCP + frontend) automatically so the
// frontend can connect without a manual click; falls back to the next
// free port if the configured one is in use.
func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
	log.Printf("Wails window started")
	if err := a.srvMgr.Start(); err != nil {
		log.Printf("auto-start HTTP server: %v", err)
	}
}

// OnShutdown is called by Wails before the window closes. Stops the
// HTTP server if it is still running.
func (a *App) OnShutdown(ctx context.Context) {
	log.Println("Wails window shutting down")
	if a.srvMgr.IsRunning() {
		if err := a.srvMgr.Stop(); err != nil {
			log.Printf("stop server on shutdown: %v", err)
		}
	}
}

// ── Server control bindings ────────────────────────────────────────

// StartServer launches the HTTP server (REST + MCP + frontend). Safe to
// call when already running (no-op).
func (a *App) StartServer() error {
	return a.srvMgr.Start()
}

// StopServer gracefully shuts down the HTTP server. Safe to call when
// not running (no-op).
func (a *App) StopServer() error {
	return a.srvMgr.Stop()
}

// IsServerRunning reports whether the HTTP server is currently accepting
// connections.
func (a *App) IsServerRunning() bool {
	return a.srvMgr.IsRunning()
}

// PublishAddr returns a browser-friendly "host:port" derived from the
// configured host/port. cfg.Host may be a wildcard ("0.0.0.0", "::") that
// is not directly routable from another device, so the host is run through
// config.ResolvePublicHost to substitute the first reachable LAN IP. The
// port comes from the actual bound listener (post-fallback) when the
// server is running, otherwise from cfg.Port. Returns "" when stopped.
func (a *App) PublishAddr() string {
	host := config.ResolvePublicHost(a.cfg.Host)
	port := strconv.Itoa(a.cfg.Port)
	if addr := a.srvMgr.Addr(); addr != "" {
		// srvMgr.Addr() returns the actual bound listener (e.g. "[::]:17880"
		// or "0.0.0.0:17881"). Split off the port so the URL reflects the
		// real listen socket, including any fallback the manager picked.
		if _, p, err := net.SplitHostPort(addr); err == nil {
			port = p
		}
	}
	return net.JoinHostPort(host, port)
}

// ── Accessory bindings ───────────────────────────────────────────

func (a *App) CreateAccessory(ctx context.Context, in domain.Accessory) (domain.Accessory, error) {
	return a.accessory.Create(ctx, in)
}

func (a *App) GetAccessory(ctx context.Context, id int64) (domain.Accessory, error) {
	return a.accessory.Get(ctx, id)
}

func (a *App) GetAccessoryByName(ctx context.Context, name string) (domain.Accessory, error) {
	return a.accessory.GetByName(ctx, name)
}

func (a *App) UpdateAccessory(ctx context.Context, id int64, u domain.AccessoryUpdate) (domain.Accessory, error) {
	return a.accessory.Update(ctx, id, u)
}

func (a *App) DeleteAccessory(ctx context.Context, id int64) error {
	return a.accessory.Delete(ctx, id)
}

func (a *App) ListAccessories(ctx context.Context, q string, limit, offset int) ([]domain.Accessory, int, error) {
	return a.accessory.List(ctx, q, limit, offset)
}

// ── Stock bindings ───────────────────────────────────────────────

func (a *App) Inbound(ctx context.Context, cmd service.InboundCmd) (domain.InventoryFlow, error) {
	return a.stock.Inbound(ctx, cmd)
}

func (a *App) Outbound(ctx context.Context, cmd service.OutboundCmd) (domain.InventoryFlow, error) {
	return a.stock.Outbound(ctx, cmd)
}

func (a *App) BatchInbound(ctx context.Context, items []service.InboundCmd) (service.BatchResult, error) {
	return a.stock.BatchInbound(ctx, items)
}

func (a *App) BatchOutbound(ctx context.Context, items []service.OutboundCmd) (service.BatchResult, error) {
	return a.stock.BatchOutbound(ctx, items)
}

// ── Flow bindings ────────────────────────────────────────────────

func (a *App) ListFlows(ctx context.Context, typ, from, to string, limit, offset int) ([]domain.InventoryFlow, int, error) {
	return a.flow.List(ctx, typ, from, to, limit, offset)
}

func (a *App) GetFlow(ctx context.Context, id int64) (domain.InventoryFlow, error) {
	return a.flow.Get(ctx, id)
}

func (a *App) ListFlowsByAccessory(ctx context.Context, accessoryID int64, typ, from, to string, limit, offset int) ([]domain.InventoryFlow, int, error) {
	return a.flow.ListByAccessory(ctx, accessoryID, typ, from, to, limit, offset)
}

// ── Replenishment bindings ───────────────────────────────────────

func (a *App) ScanShortage(ctx context.Context) ([]service.ReplenishmentItem, error) {
	return a.replenishment.Scan(ctx)
}

func (a *App) CheckReplenishment(ctx context.Context, names []string, policy string) (service.BatchCheckResult, error) {
	return a.replenishment.Check(ctx, names, policy)
}