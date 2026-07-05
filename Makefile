.PHONY: all linux windows macos dev test test-e2e test-all clean help \
        web-install web-build web-dev

APP_NAME  := warehouse
OUT_DIR   := build/bin
WAILS     := $(HOME)/go/bin/wails


all: linux windows

linux:
	@echo "=== 构建 Linux ==="
	$(WAILS) build -platform linux/amd64 -tags webkit2_41 -ldflags="-s -w"
	@echo "完成: $(OUT_DIR)/$(APP_NAME)"

windows:
	@echo "=== 构建 Windows ==="
	$(WAILS) build -platform windows/amd64 -webview2 embed -ldflags="-s -w -H windowsgui"
	@echo "完成: $(OUT_DIR)/$(APP_NAME).exe"

macos:
	@uname -s | grep -q Darwin || { echo "错误: macOS 构建必须在 Mac 上运行"; exit 1; }
	@echo "=== 构建 macOS ==="
	$(WAILS) build -ldflags="-s -w"
	@echo "完成: $(OUT_DIR)/$(APP_NAME)"

dev:
	$(WAILS) dev -tags webkit2_41

# ── 前端 ─────────────────────────────────────────────────────────
# 仅构建 / 运行前端（React + Vite + TS），不依赖 Wails。
# Wails 自身在 wails build / wails dev 时会自动调用 frontend:build。

web-install:
	@echo "=== 安装前端依赖 (pnpm) ==="
	cd frontend && pnpm install

web-build:
	@echo "=== 构建前端 (tsc + vite build) ==="
	@if [ ! -d frontend/node_modules ]; then \
		echo "缺少 frontend/node_modules，请先运行: make web-install"; \
		exit 1; \
	fi
	cd frontend && pnpm build

web-dev:
	@echo "=== 前端 dev server (Vite, 默认 :5173) ==="
	cd frontend && pnpm dev

test:
	go test ./...

test-e2e:
	go test -run E2E -count=1 -timeout 60s -v .

test-all: test test-e2e

clean:
	rm -rf $(OUT_DIR) /tmp/warehouse_e2e_test

help:
	@echo "Warehouse — Wails 桌面应用 (手机配件管理系统)"
	@echo ""
	@echo "  make all          构建全平台"
	@echo "  make linux        构建 Linux"
	@echo "  make windows      构建 Windows"
	@echo "  make macos        构建 macOS (需在 Mac 上运行)"
	@echo "  make dev          开发模式 (Wails + 热重载)"
	@echo "  make web-install  安装前端依赖 (pnpm install)"
	@echo "  make web-build    仅构建前端 (tsc + vite build)"
	@echo "  make web-dev      仅运行前端 dev server"
	@echo "  make test         单元测试"
	@echo "  make test-e2e     e2e 集成测试 (--headless)"
	@echo "  make test-all     全部测试"
	@echo "  make clean        清理产物"
	@echo ""