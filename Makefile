.PHONY: all linux windows macos dev test test-e2e test-all clean help

APP_NAME  := warehouse
OUT_DIR   := build/bin
WAILS     := $(HOME)/go/bin/wails

help:
	@echo "Warehouse — Wails 桌面应用 (手机配件管理系统)"
	@echo ""
	@echo "  make all          构建全平台"
	@echo "  make linux        构建 Linux"
	@echo "  make windows      构建 Windows"
	@echo "  make macos        构建 macOS (需在 Mac 上运行)"
	@echo "  make dev          开发模式，热重载"
	@echo "  make test         单元测试"
	@echo "  make test-e2e     e2e 集成测试 (--headless)"
	@echo "  make test-all     全部测试"
	@echo "  make clean        清理产物"
	@echo ""

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

test:
	go test ./...

test-e2e:
	go test -run E2E -count=1 -timeout 60s -v .

test-all: test test-e2e

clean:
	rm -rf $(OUT_DIR) /tmp/warehouse_e2e_test