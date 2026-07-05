# Warehouse — 手机配件管理系统

Wails (Go + WebView) 桌面应用，内嵌 HTTP 服务与 MCP 端点，覆盖配件目录、
出入库、流水、补货判断。GUI 模式自动起 Web 服务，也可在 CLI / 浏览器模式
单独使用 HTTP + MCP。

## 启动

应用有两种运行模式：**GUI**（默认，开窗口并自动起 Web 服务）和
**headless**（不开窗口，直接 HTTP + MCP，常用于 CI / 服务器场景）。

```bash
# 1. GUI：需要 Wails SDK（安装见下文）。窗口出现后
#    Web 服务自动启动，浏览器可访问同一地址。
wails dev                       # 开发模式（热重载）
make linux                      # 出 Linux GUI 二进制
make windows                    # 出 Windows GUI 二进制
make macos                      # 出 macOS GUI 二进制（需在 Mac 上）

# 2. 命令行：不需要 Wails，纯 Go 进程提供 HTTP + MCP + 嵌入式前端。
go run . --headless             # 开发
make linux && ./build/bin/warehouse --headless   # 生产
```

启动后访问 <http://127.0.0.1:17880> 即可看到前端（含 REST + MCP 端点）。

## 配置

通过命令行 flag 或同名环境变量配置，**命令行优先级更高**：

| flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--host` | `WAREHOUSE_HOST` | `127.0.0.1` | HTTP 监听地址；设为 `0.0.0.0` 开启远程访问 |
| `--port` | `WAREHOUSE_PORT` | `17880` | HTTP 监听端口 |
| `--db` | `WAREHOUSE_DB_PATH` | `./data/warehouse.db` | SQLite 数据库路径（相对当前工作目录） |
| `--headless` | — | `false` | 跳过 GUI，直接 HTTP + MCP |

### 端口占用自动顺移

如果默认端口被占用，HTTP 服务会从下一个端口开始向后顺移尝试最多 100 个
端口，命中空闲端口立即绑定。日志会输出：

```
port 17880 in use; falling back to 17881
HTTP server started on 127.0.0.1:17881
```

实际监听地址可通过 `App.ServerAddr()`（GUI 绑定）或浏览器地址栏确认。

## 端点

| 路径 | 说明 |
|---|---|
| `GET /healthz` | 健康检查，返回 `{status, version}` |
| `/api`, `/api/*` | REST API（chi 路由，详见 `internal/api`） |
| `/mcp`, `/mcp/*` | MCP 端点（SSE + messages） |
| `/*` | 嵌入式前端（SPA 路由 fallback 到 `index.html`） |

MCP 共 **13 个工具**，按域分组：

- **accessory** (5): 创建 / 查询 (id, sku) / 更新 / 删除 / 列表
- **stock** (4): 入库 / 出库 / 批量入库 / 批量出库
- **flow** (2): 列表 / 按配件查询
- **replenishment** (2): 扫描缺货 / 按 SKU 检查

## 构建

```bash
# 全平台 GUI 二进制
make all            # = linux + windows
make linux          # Linux GUI（webkit2_41）
make windows        # Windows GUI（webview2 embed）
make macos          # macOS GUI（需在 Mac 上）

# 仅前端
make web-install    # pnpm install
make web-build      # tsc + vite build → frontend/dist
make web-dev        # Vite dev server（:5173）

# 测试
make test           # go test ./...
make test-e2e       # 端到端集成（启动 --headless，跑 HTTP/MCP）
make test-all       # = test + test-e2e

make clean          # 清理 build/ 产物
```

> `make linux/windows/macos` 不需要先跑 `make web-build`：
> Wails 内部会读取 `wails.json` 的 `frontend:install` / `frontend:build`
> 钩子，自己完成前端的 install + build。`make web-build` 是给"只想
> 改前端、不想走 Wails"场景用的便捷 target。

## 前置依赖

- Go 1.22+
- Node.js 18+ + pnpm
- Wails CLI（仅 GUI 构建需要）：

  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```

  Linux 还需 `webkit2gtk-4.1-dev` 等系统包，详见
  [Wails 安装文档](https://wails.io/docs/gettingstarted/installation)。

## 项目结构

```
├── main.go                  # 入口：解析配置 → 装配 services → 启 server
├── app.go                   # Wails 启动 / 关闭 / 嵌入 frontend/dist
├── e2e_test.go              # 端到端：启动二进制跑 HTTP/MCP
├── frontend/                # React + Vite + TS 前端
│   └── src/pages/           # AccessoryList / Inbound / Outbound / Flows / Replenishment / Settings
├── internal/
│   ├── api/                 # REST（chi）
│   ├── config/              # flag + env 解析
│   ├── db/                  # SQLite + migration
│   ├── desktop/             # Wails bindings + 菜单 + 单实例 + ServerManager
│   ├── domain/              # Accessory, InventoryFlow
│   ├── logging/             # 日志初始化
│   ├── mcp/                 # MCP server（13 tools）
│   ├── repo/                # 仓储层
│   ├── service/             # 业务逻辑
│   └── webserver/           # HTTP 服务 + 嵌入前端
└── changes/                 # spec-superflow 产物
```

## 设计要点

- **GUI / CLI 共用一份 HTTP 栈**：`internal/webserver` 不依赖 Wails，
  桌面 / headless 都通过 `desktop.ServerManager` 启停。
- **GUI 自动起 HTTP**：`App.OnStartup` 自动调用 `ServerManager.Start()`，
  端口冲突时向后顺移。前端的"启动 / 停止 Web 服务"按钮仍可手动控制。
- **前端通过 `//go:embed all:frontend/dist` 烧入二进制**：CI 出包时
  Wails 自动跑 `pnpm build`，无需手动 `make web-build`。
- **静态文件自动定位**：InitStatic 会从 embed 根开始 BFS 找 `index.html`，
  因此前端目录即使再嵌套一层（如 `frontend/dist/`）也能正确服务。