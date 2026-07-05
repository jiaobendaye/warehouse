# 实现任务：手机配件管理系统

> 工作流模式：`full` · 8 个 capability · 13 个实现批次。
> 每批内部遵循 TDD Iron Law（1.1 失败测试 → 1.2 确认失败 → 1.3 最小实现 → 1.4 确认通过 → 1.5 提交）。

## 文件结构

### 后端（Go）

- `Create: go.mod` — Go 模块声明（`module github.com/jiaobendaye/warehouse`）
- `Create: main.go` — 入口：解析 flag → 分发到 GUI / web-only / mcp-stdio / 全形态
- `Create: app.go` — Wails App struct、OnStartup、bindings 注册
- `Create: internal/config/config.go` — 解析 flag/env，承载 host/port/db path/启动模式
- `Create: internal/db/db.go` — SQLite 连接（`modernc.org/sqlite`）、WAL、连接池
- `Create: internal/db/migrate.go` — 读取并执行 `migrations/*.sql`
- `Create: migrations/0001_init.sql` — `accessories` / `inventory_flow` 表 + 索引
- `Create: internal/domain/accessory.go` — `Accessory` struct + 校验
- `Create: internal/domain/flow.go` — `InventoryFlow` struct + 枚举
- `Create: internal/repo/accessory_repo.go` — 配件 CRUD（SQL）
- `Create: internal/repo/flow_repo.go` — 流水读写
- `Create: internal/repo/accessory_repo_test.go` — 仓储层测试（使用临时 SQLite）
- `Create: internal/repo/flow_repo_test.go` — 流水仓储测试
- `Create: internal/service/accessory_service.go` — 配件业务逻辑（去重、阈值校验）
- `Create: internal/service/stock_service.go` — 出入库 + 事务 + 幂等
- `Create: internal/service/flow_service.go` — 流水查询
- `Create: internal/service/replenishment_service.go` — 告急/补货
- `Create: internal/service/accessory_service_test.go` — 服务层单测
- `Create: internal/service/stock_service_test.go` — 含事务回滚与幂等测试
- `Create: internal/service/flow_service_test.go`
- `Create: internal/service/replenishment_service_test.go`
- `Create: internal/api/router.go` — chi 路由注册
- `Create: internal/api/middleware.go` — recover / CORS / 请求日志
- `Create: internal/api/accessory_handler.go` — `/api/v1/accessories*` handlers
- `Create: internal/api/stock_handler.go` — `/api/v1/stock/inbound|outbound|batch_*`
- `Create: internal/api/flow_handler.go` — `/api/v1/flows*`
- `Create: internal/api/replenishment_handler.go` — `/api/v1/replenishment*`
- `Create: internal/api/errors.go` — 统一错误响应格式 `{error:{code,message}}`
- `Create: internal/api/router_test.go` — `httptest` 端到端测试
- `Create: internal/mcp/server.go` — MCP server 注册 + 工具清单
- `Create: internal/mcp/tools_accessory.go` — `accessory.*` 工具实现
- `Create: internal/mcp/tools_stock.go` — `stock.*` 工具实现
- `Create: internal/mcp/tools_flow.go` — `flow.*` 工具实现
- `Create: internal/mcp/tools_replenishment.go` — `replenishment.*` 工具实现
- `Create: internal/mcp/server_test.go` — MCP 工具调用测试
- `Create: internal/webserver/server.go` — `http.Server` 生命周期、`/healthz`、embed.FS
- `Create: internal/webserver/static.go` — `//go:embed all:web/dist` 静态文件服务
- `Create: internal/webserver/cors.go` — CORS 中间件
- `Create: internal/desktop/bindings.go` — 把 service 方法暴露给 Wails frontend
- `Create: internal/desktop/menu.go` — 原生菜单（打开主窗口、在浏览器打开、退出）
- `Create: internal/desktop/singleinstance.go` — 跨平台单实例锁

### 前端（Vite + React + TS）

- `Create: web/package.json` — 前端依赖与脚本
- `Create: web/vite.config.ts` — Vite 配置（开发代理 `/api` 到本地 17880）
- `Create: web/tsconfig.json`
- `Create: web/index.html`
- `Create: web/src/main.tsx` — 入口 + 路由
- `Create: web/src/api/client.ts` — 通信适配层（探测 Wails 否则 fetch）
- `Create: web/src/api/accessory.ts` — 配件 API 封装
- `Create: web/src/api/stock.ts` — 出入库 API
- `Create: web/src/api/flow.ts` — 流水 API
- `Create: web/src/api/replenishment.ts`
- `Create: web/src/pages/AccessoryList.tsx` — 配件列表/CRUD
- `Create: web/src/pages/Inbound.tsx` — 入库（单 + 批量）
- `Create: web/src/pages/Outbound.tsx` — 出库（单 + 批量）
- `Create: web/src/pages/Flows.tsx` — 流水查询
- `Create: web/src/pages/Replenishment.tsx` — 告急面板
- `Create: web/src/components/Toast.tsx` — 通用错误提示

### 配置 / 文档

- `Create: wails.json` — Wails CLI 配置
- `Create: README.md` — 三形态启动说明
- `Create: .gitignore` — 忽略 `web/dist`（由 CI/构建生成）、`build/bin/*`、本地 db

## 接口

### Batch 2 → Batch 3
- **Produces**: `Accessory` (`internal/domain/accessory.go`) — `id, sku, name, unit, current_stock, low_stock_threshold, notes, created_at, updated_at`
- **Produces**: `InventoryFlow` (`internal/domain/flow.go`) — `id, accessory_id, type, quantity, unit_cost, unit_price, balance_after, client_ref, remark, occurred_at, created_at`

### Batch 3 → Batch 4
- **Produces**: `AccessoryService` — `Create(ctx, Accessory) (Accessory, error)`, `Get(ctx, id) (Accessory, error)`, `GetBySKU(...)`, `List(ctx, q, limit, offset) ([]Accessory, total int, error)`, `Update(...)`, `Delete(ctx, id) error`
- **Consumes**: `AccessoryRepo`, `Accessory` 校验规则

### Batch 4 → Batch 5/6
- **Produces**: `StockService` — `Inbound(ctx, InboundCmd) (Flow, error)`, `Outbound(...)`, `BatchInbound(ctx, []InboundCmd) ([]Flow, error)`, `BatchOutbound(...)`
- **Consumes**: `AccessoryRepo`（库存读改）、`FlowRepo`（写入流水）

### Batch 5/6 → Batch 7/8
- **Produces**: `FlowService.List/Get`, `ReplenishmentService.Scan/Check`
- **Consumes**: `AccessoryRepo`, `FlowRepo`

### Batch 7/8/9 → Batch 10/11
- **Produces**:
  - REST endpoints: `/api/v1/accessories[/:id]`, `/api/v1/stock/{inbound,outbound,batch_inbound,batch_outbound}`, `/api/v1/flows[/:id]`, `/api/v1/replenishment/{scan,check}`
  - MCP tools: 13 个（见 `specs/mcp-server.md`）
  - Static assets at `/` (from `web/dist`)
- **Consumes**: 全部 service

### Batch 12 → Batch 13
- **Produces**: Wails App 注册的 bindings（前端通过 `window.go.main.AccessoryService.*` 调用）
- **Consumes**: 全部 service

---

## 1. Batch 1: 项目脚手架与 SQLite 基础

- [ ] **1.1 编写失败的测试**

```go
// internal/db/db_test.go
func TestOpen_CreatesFileAndAppliesMigrations(t *testing.T) {
    dir := t.TempDir()
    dbPath := filepath.Join(dir, "test.db")
    db, err := db.Open(dbPath)
    if err != nil { t.Fatal(err) }
    defer db.Close()
    // 期望：表 accessories 存在
    var name string
    if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='accessories'`).Scan(&name); err != nil {
        t.Fatalf("expected accessories table: %v", err)
    }
    if name != "accessories" { t.Fatalf("got %q", name) }
}
```

**Files**: `Create: internal/db/db_test.go`

- [ ] **1.2 运行测试并确认失败**

Run: `go test ./internal/db/...`
Expected: FAIL with "package internal/db: no Go files"

- [ ] **1.3 实现最小化代码**

- `internal/db/db.go`：`Open(path string) (*sql.DB, error)` 打开 SQLite，开启 WAL、外键、`_pragma=journal_mode(WAL)`
- `internal/db/migrate.go`：`Migrate(db *sql.DB, fs fs.FS) error` 读取 `migrations/*.sql` 排序执行
- `migrations/0001_init.sql`：`accessories`、`inventory_flow` 表 + 索引（`UNIQUE(sku)`、`INDEX(accessory_id, occurred_at)`、`UNIQUE(client_ref) WHERE client_ref IS NOT NULL`）
- `go.mod`：`go 1.22`、`require modernc.org/sqlite` 与 `github.com/google/uuid`
- `.gitignore`：忽略 `build/bin/*`、`*.db`、`web/dist`（开发期）
- `README.md`：占位说明

**Files**: `Create: go.mod`, `Create: internal/db/db.go`, `Create: internal/db/migrate.go`, `Create: migrations/0001_init.sql`, `Create: .gitignore`, `Create: README.md`

- [ ] **1.4 运行测试并确认通过**

Run: `go test ./internal/db/...`
Expected: PASS

- [ ] **1.5 提交**

```bash
git add internal/db migrations go.mod .gitignore README.md
git commit -m "feat(db): sqlite connection + initial migration"
```

---

## 2. Batch 2: 领域模型与仓储层

- [ ] **2.1 编写失败的测试**

```go
// internal/repo/accessory_repo_test.go
func TestAccessoryRepo_CreateAndGetBySKU(t *testing.T) {
    db := newTestDB(t)
    r := repo.NewAccessoryRepo(db)
    a, err := r.Create(ctx, domain.Accessory{SKU: "SKU-1", Name: "壳", Unit: "个", LowStockThreshold: 5})
    if err != nil { t.Fatal(err) }
    got, err := r.GetBySKU(ctx, "SKU-1")
    if err != nil { t.Fatal(err) }
    if got.Name != "壳" { t.Fatalf("got %+v", got) }
}
```

**Files**: `Create: internal/repo/accessory_repo_test.go`, `Create: internal/repo/flow_repo_test.go`

- [ ] **2.2 运行测试并确认失败**

Run: `go test ./internal/repo/...`
Expected: FAIL with "package internal/repo: no Go files"

- [ ] **2.3 实现最小化代码**

- `internal/domain/accessory.go`：`Accessory` struct + `Validate()`（SKU 非空、Threshold>=0）
- `internal/domain/flow.go`：`InventoryFlow`、`FlowType` 枚举（`in`/`out`）
- `internal/repo/accessory_repo.go`：`Create/Get/GetBySKU/List(q,limit,offset)/Update/Delete`
- `internal/repo/flow_repo.go`：`Insert(tx, flow)`、`GetByID`、`List(...)`（支持 accessory_id/type/from/to/limit/offset）

**Files**: `Create: internal/domain/accessory.go`, `Create: internal/domain/flow.go`, `Create: internal/repo/accessory_repo.go`, `Create: internal/repo/flow_repo.go`

- [ ] **2.4 运行测试并确认通过**

Run: `go test ./internal/repo/... ./internal/domain/...`
Expected: PASS

- [ ] **1.5 提交**

```bash
git add internal/domain internal/repo
git commit -m "feat(repo): accessory and flow repositories with tests"
```

---

## 3. Batch 3: AccessoryService

- [ ] **3.1 编写失败的测试**

```go
// internal/service/accessory_service_test.go
func TestAccessoryService_Create_DuplicateSKU(t *testing.T) {
    s := newTestService(t)
    _, _ = s.Accessory.Create(ctx, domain.Accessory{SKU: "A", Name: "x", Unit: "个"})
    _, err := s.Accessory.Create(ctx, domain.Accessory{SKU: "A", Name: "y", Unit: "个"})
    if !errors.Is(err, service.ErrSKUConflict) { t.Fatalf("got %v", err) }
}

func TestAccessoryService_Update_RejectsSKUChange(t *testing.T) { /* ... */ }
```

**Files**: `Create: internal/service/accessory_service_test.go`

- [ ] **3.2 运行测试并确认失败**

Run: `go test ./internal/service/...`
Expected: FAIL

- [ ] **3.3 实现最小化代码**

- `internal/service/accessory_service.go`：把仓储调用包成业务方法，定义 `ErrSKUConflict`、`ErrNotFound`、`ErrInvalidInput`
- Delete 时校验是否存在流水（依赖 FlowRepo.CountByAccessory）；若存在返回 `ErrHasFlow`

**Files**: `Create: internal/service/accessory_service.go`

- [ ] **3.4 运行测试并确认通过**

Run: `go test ./internal/service/...`
Expected: PASS

- [ ] **3.5 提交**

```bash
git add internal/service
git commit -m "feat(service): accessory service with validation"
```

**Depends on**: Batch 2

---

## 4. Batch 4: StockService（出入库 + 批量 + 幂等）

- [ ] **4.1 编写失败的测试**

```go
func TestStockService_Inbound_UpdatesStockAndFlow(t *testing.T)
func TestStockService_Inbound_IdempotentByClientRef(t *testing.T)
func TestStockService_Outbound_InsufficientStock(t *testing.T)
func TestStockService_BatchInbound_AllOrNothing(t *testing.T)
func TestStockService_BatchOutbound_PartialFailureRollback(t *testing.T)
```

**Files**: `Create: internal/service/stock_service_test.go`

- [ ] **4.2 运行测试并确认失败**

Run: `go test ./internal/service/...`
Expected: FAIL on stock_service_test

- [ ] **4.3 实现最小化代码**

- `internal/service/stock_service.go`：
  - `Inbound(ctx, InboundCmd)`: tx → update stock → insert flow → 计算 `balance_after`
  - 幂等：`client_ref` 非空时先查 flow_repo.GetByClientRef，存在则直接返回
  - `Outbound`：同上，但库存不足返回 `ErrInsufficientStock`
  - `BatchInbound/Outbound`：单 tx 内循环，任一失败整体回滚

**Files**: `Create: internal/service/stock_service.go`

- [ ] **4.4 运行测试并确认通过**

Run: `go test ./internal/service/...`
Expected: PASS

- [ ] **4.5 提交**

```bash
git add internal/service/stock_service.go
git commit -m "feat(service): stock service with batch + idempotency"
```

**Depends on**: Batch 3

---

## 5. Batch 5: FlowService

- [ ] **5.1 编写失败的测试**：`TestFlowService_ListByAccessoryAndType`、`TestFlowService_GlobalList`、`TestFlowService_Get_NotFound`
- [ ] **5.2 运行失败**：`go test ./internal/service/...`
- [ ] **5.3 实现**：`internal/service/flow_service.go` 封装 FlowRepo + 类型/时间区间解析
- [ ] **5.4 通过**
- [ ] **5.5 提交**：`feat(service): flow query service`

**Depends on**: Batch 2

---

## 6. Batch 6: ReplenishmentService

- [x] **6.1 编写失败的测试**：`TestScan_FindsShortageItems`、`TestScan_ExcludesThresholdZero`、`TestCheck_PartialShortage`、`TestCheck_ReportsMissingSKUs`、`TestCheck_FixedPolicy`
- [x] **6.2 运行失败**
- [x] **6.3 实现**：`internal/service/replenishment_service.go`
- [x] **6.4 通过**
- [x] **6.5 提交**：`feat(service): replenishment advisor`

**Depends on**: Batch 3

---

## 7. Batch 7: REST API（chi）

- [ ] **7.1 编写失败的测试**

```go
// internal/api/router_test.go
func TestRouter_AccessoriesListAndCreate(t *testing.T)
func TestRouter_StockInbound_AndFlowAppears(t *testing.T)
func TestRouter_Outbound_InsufficientStock_409(t *testing.T)
func TestRouter_ErrorsHaveUnifiedShape(t *testing.T)
```

**Files**: `Create: internal/api/router_test.go`

- [ ] **7.2 运行失败**：`go test ./internal/api/...`
- [ ] **7.3 实现**：
  - `internal/api/router.go`：`New(services) http.Handler`，注册所有 `/api/v1/*` 端点
  - `internal/api/errors.go`：`WriteError(w, status, code, msg)` 统一响应
  - `internal/api/middleware.go`：`Recover`、`CORS`、`RequestLog`
  - 4 个 handler 文件：把 service 调用翻译为 HTTP I/O
- [ ] **7.4 通过**：`go test ./internal/api/...`
- [ ] **7.5 提交**：`feat(api): rest endpoints with unified error shape`

**Depends on**: Batch 3/4/5/6

---

## 8. Batch 8: MCP Server

- [ ] **8.1 编写失败的测试**：`TestMCP_ToolsList`、`TestMCPCall_AccessoryGet`、`TestMCPCall_StockOutbound_Insufficient`、`TestMCP_StdioRoundtrip`（在进程内启动 stdio server 用 stdin/stdout pipe 模拟客户端）
- [ ] **8.2 运行失败**
- [ ] **8.3 实现**：
  - `internal/mcp/server.go`：`New(services) *mcp.Server`、注册 13 个工具
  - `internal/mcp/tools_*.go`：把每个 service 方法包装为 tool handler，参数通过 JSON Schema 描述
  - 错误映射：`ErrNotFound→-32004`、`ErrConflict→-32005`、`ErrInvalidInput→-32600`
- [ ] **8.4 通过**
- [ ] **8.5 提交**：`feat(mcp): server with 13 tools and error mapping`

**Depends on**: Batch 3/4/5/6

---

## 9. Batch 9: WebServer（HTTP 主机 + 静态前端 + 健康检查）

- [ ] **9.1 编写失败的测试**：`TestWebServer_Healthz`、`TestWebServer_ServesEmbeddedIndex`、`TestWebServer_RejectsRemoteByDefault`、`TestWebServer_AllowsRemoteWithHostFlag`
- [ ] **9.2 运行失败**
- [ ] **9.3 实现**：
  - `internal/webserver/server.go`：`Server{Addr, Handler}`、`Start(ctx)`、`Shutdown(ctx)`
  - `internal/webserver/static.go`：`//go:embed all:web/dist` 把 Vite 构建产物作为文件系统
  - `internal/webserver/cors.go`：默认同源；`--host 0.0.0.0` 时按 `WAREHOUSE_ALLOWED_ORIGINS` 列表允许
  - 根路径 `/` → `index.html`；`/assets/*` 直通；其余非 `/api`、`/mcp`、`/healthz` 返回 SPA 兜底 `index.html`
- [ ] **9.4 通过**
- [ ] **9.5 提交**：`feat(web): embedded http server with static + cors`

**Depends on**: Batch 7/8

---

## 10. Batch 10: 前端骨架（Vite + React + 通信适配）

- [ ] **10.1 编写失败的测试**

```ts
// web/src/api/client.test.ts
import { isWails, getTransport } from './client';
test('isWails returns false when window.runtime missing', () => {
    delete (window as any).runtime;
    expect(isWails()).toBe(false);
    expect(getTransport()).toBe('http');
});
```

**Files**: `Create: web/src/api/client.test.ts`

- [ ] **10.2 运行失败**：`pnpm test`
- [ ] **10.3 实现**：
  - `web/package.json`：react 18、vite 5、typescript 5、vitest、react-router-dom 6
  - `web/vite.config.ts`：dev server 代理 `/api` → `http://127.0.0.1:17880`，`build.outDir: 'dist'`
  - `web/src/main.tsx`：`createRoot` + `<App />`
  - `web/src/App.tsx`：路由 `/accessories`、`/inbound`、`/outbound`、`/flows`、`/replenishment`
  - `web/src/api/client.ts`：`isWails()` 探测 `window.runtime`；`apiCall(name, args)` 路由到 Wails 或 fetch
  - 各 `api/*.ts`：薄封装
- [ ] **10.4 通过**：`pnpm test`、`pnpm build`
- [ ] **10.5 提交**：`feat(web): react skeleton with transport adapter`

**Depends on**: 无（前端可并行）

---

## 11. Batch 11: 前端页面（CRUD + 出入库 + 流水 + 告急）

- [ ] **11.1 编写组件测试**（vitest + @testing-library/react）：
  - `<AccessoryList />` 渲染列表、点击删除触发确认
  - `<Inbound />` 提交表单调用 `stockInbound` 并 toast 成功
  - `<Outbound />` 库存不足显示错误（mock API 返回 409）
  - `<Replenishment />` 渲染告急列表
- [ ] **11.2 运行失败**
- [ ] **11.3 实现 5 个页面 + Toast 组件**
- [ ] **11.4 通过**
- [ ] **11.5 提交**：`feat(web): pages for accessory/inbound/outbound/flows/replenishment`

**Depends on**: Batch 10

---

## 12. Batch 12: 桌面入口（Wails App + bindings + 菜单 + 单实例）

- [ ] **12.1 编写测试**：
  - `internal/desktop/bindings_test.go`：验证 `AccessoryBinding.Create` 调用 service.Create
  - `internal/desktop/singleinstance_test.go`：第二次 `TryLock` 返回 `ErrAlreadyRunning`
- [ ] **12.2 运行失败**
- [ ] **12.3 实现**：
  - `app.go`：`App` struct 持有 services、`OnStartup(ctx)` 启动 webserver + MCP http
  - `internal/desktop/bindings.go`：把 service 方法暴露为 `App.AccessoryService.List(...)` 等（与 Wails 自动绑定兼容的导出名）
  - `internal/desktop/menu.go`：菜单项 `打开主窗口`、`在浏览器中打开`、`退出`
  - `internal/desktop/singleinstance.go`：`~/.warehouse.lock` flock；二次启动返回错误并通过 stdin 通知原进程前置窗口
  - `main.go`：`flag.NewFlagSet` 解析 `--host`、`--port`、`--web-only`、`--mcp-stdio`、`--db`；根据 flag 决定启动模式
- [ ] **12.4 通过**
- [ ] **12.5 提交**：`feat(desktop): wails app entry, menu, single-instance`

**Depends on**: Batch 9/11

---

## 13. Batch 13: 集成与冒烟

- [ ] **13.1 编写 e2e 测试（Go）**：`TestE2E_GUIAndWebShareSameData`
  - 起进程（仅 web 模式）→ 通过 REST 创建配件 → 通过 MCP `accessory.get` 取到 → 校验一致
- [ ] **13.2 运行失败**
- [ ] **13.3 补足任何 e2e 缺口**
- [ ] **13.4 通过**：`go test ./...`
- [ ] **13.5 提交**：`test(e2e): dual-mode parity check`

**Depends on**: Batch 12

---

## 关键依赖图

```
Batch 1 (db)
   └── Batch 2 (repo + domain)
          ├── Batch 3 (accessory svc)
          │     ├── Batch 4 (stock svc)
          │     ├── Batch 5 (flow svc)
          │     └── Batch 6 (replenishment svc)
          │            └── Batch 7 (REST)
          │            └── Batch 8 (MCP)
          │                   └── Batch 9 (webserver)
          │                          └── Batch 12 (desktop)
          └── Batch 10 (web skeleton) ── Batch 11 (web pages) ──┘
                                                                       Batch 13 (e2e)
```

## 验证维度

| 维度 | 验证方式 |
|---|---|
| Completeness | `go test ./...` 全部通过；每个 spec 的 Scenario 至少有一个对应测试 |
| Correctness | 事务回滚测试（批量出入库任一失败全回滚）；幂等测试（同 client_ref 不重复扣库存） |
| Coherence | 双形态数据一致性 e2e（REST 写入 → MCP 读到一致数据） |
| Build | `wails build` 三个平台成功；`go build ./...` 无 warning |

## Review Gates

- **强制审查点**：Batch 4（事务与幂等）、Batch 8（MCP 错误映射）、Batch 12（桌面入口与单实例）。
- **阻塞类别**：事务回滚不完整、幂等失效、单实例不工作、MCP 协议不符。