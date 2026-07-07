// Warehouse — Wails desktop application for mobile-accessories management.
// Default mode opens a GUI window; use --headless for HTTP+MCP without GUI.
package main

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/jiaobendaye/warehouse/internal/api"
	"github.com/jiaobendaye/warehouse/internal/config"
	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/desktop"
	"github.com/jiaobendaye/warehouse/internal/logging"
	mcp "github.com/jiaobendaye/warehouse/internal/mcp"
	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
	"github.com/jiaobendaye/warehouse/internal/webserver"
)

type Services struct {
	Accessory     *service.AccessoryService
	Stock         *service.StockService
	Flow          *service.FlowService
	Replenishment *service.ReplenishmentService
}

func main() {
	cfg := config.Parse(nil)

	logDir := filepath.Dir(cfg.DBPath)
	if _, err := logging.Init(logDir); err != nil {
		log.Fatalf("init logging: %v", err)
	}
	defer logging.Close()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	accRepo := repo.NewAccessoryRepo(database)
	flowRepo := repo.NewFlowRepo(database)

	svcs := Services{
		Accessory:     service.NewAccessoryService(accRepo, flowRepo),
		Stock:         service.NewStockService(accRepo, flowRepo, database),
		Flow:          service.NewFlowService(flowRepo),
		Replenishment: service.NewReplenishmentService(accRepo),
	}

	exportsDir := filepath.Join(logDir, "exports")
	// When --host is the wildcard 0.0.0.0 we don't know which local IP an
	// external client should use to reach us, so pick one at startup. The
	// returned URL is what MCP export tools embed in download links.
	publicHost := config.ResolvePublicHost(cfg.Host)
	publicBaseURL := fmt.Sprintf("http://%s:%d", publicHost, cfg.Port)
	if publicHost != cfg.Host {
		log.Printf("public host resolved: %s (bind: %s)", publicHost, cfg.Host)
	}

	apiHandler := api.NewRouter(api.Services{
		Accessory:     svcs.Accessory,
		Stock:         svcs.Stock,
		Flow:          svcs.Flow,
		Replenishment: svcs.Replenishment,
		ExportsDir:    exportsDir,
	}, api.RouterOptions{})

	mcpSrv := mcp.NewServer(mcp.Services{
		Accessory:     svcs.Accessory,
		Stock:         svcs.Stock,
		Flow:          svcs.Flow,
		Replenishment: svcs.Replenishment,
		ExportsDir:    exportsDir,
		PublicBaseURL: publicBaseURL,
	})
	mcpHandler := mcp.Handler(mcpSrv)

	webserver.InitStatic(frontendAssets)

	srvMgr := desktop.NewServerManager(desktop.ServerConfig{
		Host:       cfg.Host,
		Port:       cfg.Port,
		APIHandler: apiHandler,
		MCPHandler: mcpHandler,
	})

	startGUI(cfg, srvMgr, svcs)
}