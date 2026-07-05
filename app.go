package main

import (
	"context"
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/jiaobendaye/warehouse/internal/config"
	"github.com/jiaobendaye/warehouse/internal/desktop"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

// startGUI launches the Wails desktop window. In headless mode it starts
// HTTP+MCP directly and blocks until SIGINT.
func startGUI(cfg config.Config, srvMgr *desktop.ServerManager, svcs Services) {
	app := desktop.NewApp(srvMgr, desktop.Services{
		Accessory:     svcs.Accessory,
		Stock:         svcs.Stock,
		Flow:          svcs.Flow,
		Replenishment: svcs.Replenishment,
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.SetContext(runCtx)
	app.SetConfig(cfg)

	if cfg.Headless {
		runHeadless(srvMgr)
		return
	}

	if err := wails.Run(&options.App{
		Title:     "手机配件管理系统",
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
			// Forward /api/*, /mcp/* and /healthz from the WebView to the
			// embedded HTTP server so GUI mode uses the same backend as
			// browser mode (no need for a separate Wails-binding adapter).
			Handler: desktop.NewAPIProxy(srvMgr.Addr),
		},
		OnStartup:  app.OnStartup,
		OnShutdown: app.OnShutdown,
		Bind: []interface{}{
			app,
		},
		Menu: desktop.BuildMenu(app),
	}); err != nil {
		log.Printf("Wails exited: %v", err)
	}
}

// runHeadless starts HTTP+MCP without a GUI window. Used by --headless
// flag for CI/e2e/headless-server scenarios.
func runHeadless(srvMgr *desktop.ServerManager) {
	if err := srvMgr.Start(); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Printf("headless mode — HTTP server on %s (REST + MCP + frontend)", srvMgr.Addr())
	<-ctx.Done()
	log.Println("shutting down…")
	_ = srvMgr.Stop()
	os.Exit(0)
}