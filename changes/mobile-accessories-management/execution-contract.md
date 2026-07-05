# 执行合同

## Intent Lock

- **变更名称**：`mobile-accessories-management`
- **要解决的问题**：在空仓库从零搭建"手机配件进销存"桌面应用，让小型门店/个人能离线完成配件出入库、流水追溯与告急/补货判断；同一进程既提供 GUI 又提供本地 HTTP 服务，并通过 MCP 让外部 AI 代理也能调用。
- **范围内**：
  - Wails（Go + WebView）桌面应用；同进程 HTTP 服务。
  - 配件目录 CRUD（含 `low_stock_threshold`）。
  - 入/出库（单条 + 批量）+ 出入库流水记录 + `client_ref` 幂等。
  - 流水查询（按配件 / 全局 / 时间区间 / 类型）。
  - 告急扫描（`current_stock < threshold`，阈值 0 豁免）+ 给定 SKU 批次判断 + `policy=fixed:N`。
  - MCP Server（stdio + HTTP/SSE，13 个 tool）+ REST API（`/api/v1/*`，统一错误响应）。
  - 前端单代码库（React+Vite+TS），通信层探测 Wails 否则 fetch。
  - SQLite（`modernc.org/sqlite`）+ migrations；默认监听 `127.0.0.1:17880`，远程访问需 `--host 0.0.0.0` 显式开启。
- **范围外**：
  - 账号、登录、权限、多租户。
  - 审计日志、消息推送、移动端 App。
  - 报表导出（CSV/PDF）、图表可视化。
  - 远程数据库、云端备份、电商平台对接。
  - 复杂条码/扫码硬件集成（仅留接口位）。
  - 多语言 i18n（v1 仅中文 UI）。

## Approved Behavior

- **已批准需求摘要**（共 36 条 SHALL/MUST）：
  - accessory-catalog（5）：创建/查询/更新/删除配件、阈值字段语义
  - stock-inbound（4）：单条入库、批量入库、流水记录、`client_ref` 幂等
  - stock-outbound（4）：单条出库、批量出库、流水记录、`client_ref` 幂等
  - inventory-flow（4）：按配件查询、全局查询、单条详情、不可变
  - replenishment-advisor（4）：全量扫描、批次判断、阈值 0 豁免、fixed 策略
  - mcp-server（4）：13 个 tool、stdio + HTTP/SSE、错误映射、默认仅本机
  - desktop（5）：原生窗口、Wails IPC、原生菜单、单实例、与 Web 同源
  - web-server（6）：进程内 HTTP、静态托管、REST v1、CORS、配置项、`/healthz`

- **关键场景**（不可妥协）：
  - 批量出入库任一失败 MUST 整体回滚（事务原子性）。
  - 同 `client_ref` 重复提交 MUST 返回首次结果，不重复扣库存（幂等性）。
  - 库存不足出库 MUST 返回 `409 INSUFFICIENT_STOCK` 且不修改数据。
  - `low_stock_threshold=0` 的配件 MUST 不出现在告急列表。
  - MCP 错误码映射固定：`NOT_FOUND=-32004`、`CONFLICT=-32005`、`BAD_REQUEST=-32600`。
  - 默认仅 `127.0.0.1` 可达；远程访问 MUST 显式 `--host 0.0.0.0` 并打 WARN。

- **验收检查**：
  - `go test ./...` 全绿。
  - 前端 `pnpm test` + `pnpm build` 通过；产物被 Go `embed.FS` 打入二进制。
  - e2e：同一进程以 web 模式启动，REST 创建配件后通过 MCP `accessory.get` 读到一致数据。
  - `wails build` 在 macOS / Windows / Linux 三平台产物可启动。
  - 每个 spec 的每条 Scenario 至少有一个对应测试（review 时逐条核对）。

## Design Constraints

- **架构约束**：
  - 单一 Go 进程同时承载：Wails GUI（WebView） + `http.Server`（静态前端/REST/MCP over HTTP） + 可选 stdio MCP server。
  - 业务服务层（`AccessoryService` / `StockService` / `FlowService` / `ReplenishmentService`）被 Wails bindings、REST handlers、MCP tools 三处共享。
  - 启动模式由 flag 分发：`默认(GUI+Web)` / `--web-only` / `--mcp-stdio` / 隐式全形态。
  - 单实例：GUI 模式下第二次启动 MUST 激活已有窗口（`~/.warehouse.lock` + 平台通知）。

- **接口约束**：
  - REST 路径前缀 `/api/v1`；统一错误响应 `{ "error": { "code": "...", "message": "..." } }`。
  - MCP 工具命名固定（见 `specs/mcp-server.md` 表格）；HTTP/SSE 端点 `/mcp`。
  - 前端通信：`window.runtime` 存在则走 Wails bindings；否则 `fetch('/api/v1/...')`。
  - 配置参数：`--host` / `--port` / `--db` / `--web-only` / `--mcp-stdio`；同义环境变量 `WAREHOUSE_HOST` / `WAREHOUSE_PORT` / `WAREHOUSE_DB`。
  - 健康检查 `GET /healthz` MUST 返回 `{ "status": "ok", "version": "..." }`。

- **依赖约束**：
  - Go 1.22+；`modernc.org/sqlite`（pure-Go，禁 CGO）。
  - MCP：`github.com/modelcontextprotocol/go-sdk`（官方 Go SDK）。
  - HTTP 路由：`github.com/go-chi/chi/v5`。
  - 前端：React 18 + Vite 5 + TypeScript 5；测试用 Vitest + @testing-library/react。
  - 不引入 ORM（GORM/sqlx）；直接用 `database/sql`。

- **数据约束**：
  - SQLite WAL 模式开启；外键约束开启。
  - 表结构在 `migrations/0001_init.sql`：
    - `accessories(id, sku UNIQUE, name, unit, current_stock, low_stock_threshold, notes, created_at, updated_at)`
    - `inventory_flow(id, accessory_id, type, quantity, unit_cost, unit_price, balance_after, client_ref, remark, occurred_at, created_at)`，`UNIQUE(client_ref) WHERE client_ref IS NOT NULL`，`INDEX(accessory_id, occurred_at)`
  - 数据库默认路径 `~/.warehouse/data.db`；启动参数 `--db` 覆盖。
  - 流水为审计来源，禁止修改/删除。

## Task Batches

### Batch 1：项目脚手架 + SQLite 基础
- **目标**：可编译运行的 Go 模块 + SQLite 连接 + 初始 migration。
- **输入**：空仓库。
- **输出**：`go.mod`、`internal/db/{db,migrate}.go`、`migrations/0001_init.sql`、`.gitignore`、`README.md`。
- **完成标准**：`go test ./internal/db/...` 通过；`accessories` 与 `inventory_flow` 表存在。

### Batch 2：领域模型 + 仓储层
- **目标**：领域 struct 与 CRUD/查询的 SQL 仓储。
- **输入**：Batch 1 的 DB。
- **输出**：`internal/domain/{accessory,flow}.go`、`internal/repo/{accessory_repo,flow_repo}.go` + 测试。
- **完成标准**：`go test ./internal/repo/... ./internal/domain/...` 通过。

### Batch 3：AccessoryService
- **目标**：配件业务逻辑（SKU 唯一、阈值校验、删除前流水校验）。
- **输入**：Batch 2 仓储。
- **输出**：`internal/service/accessory_service.go` + 测试。
- **完成标准**：`accessory-catalog` 5 条需求全有对应测试且通过。

### Batch 4：StockService（出入库 + 批量 + 幂等）
- **目标**：单/批出入库事务、流水写入、`client_ref` 幂等。
- **输入**：Batch 3。
- **输出**：`internal/service/stock_service.go` + 测试。
- **完成标准**：
  - 单条入/出库 happy path + 库存不足 409 + 整体回滚（任一失败全回滚）+ 同 `client_ref` 二次提交返回首次结果。

### Batch 5：FlowService
- **目标**：流水查询封装。
- **输入**：Batch 2。
- **输出**：`internal/service/flow_service.go` + 测试。
- **完成标准**：`inventory-flow` 4 条需求对应测试通过。

### Batch 6：ReplenishmentService
- **目标**：告急扫描 + 批次判断 + fixed 策略。
- **输入**：Batch 3。
- **输出**：`internal/service/replenishment_service.go` + 测试。
- **完成标准**：`replenishment-advisor` 4 条需求对应测试通过；阈值 0 配件不出现在告急列表。

### Batch 7：REST API（chi）
- **目标**：`/api/v1/*` 端点 + 统一错误响应 + recover/CORS/请求日志。
- **输入**：Batch 3-6 服务。
- **输出**：`internal/api/{router,middleware,errors,accessory_handler,stock_handler,flow_handler,replenishment_handler}.go` + 测试。
- **完成标准**：`router_test.go` 通过；所有端点返回统一错误结构；批量出入库 HTTP 层也走事务。

### Batch 8：MCP Server
- **目标**：13 个 tool + stdio + HTTP/SSE + 错误映射。
- **输入**：Batch 3-6 服务。
- **输出**：`internal/mcp/{server,tools_accessory,tools_stock,tools_flow,tools_replenishment}.go` + 测试。
- **完成标准**：
  - `tools/list` 返回 13 个 tool。
  - 进程内 stdio roundtrip 测试通过。
  - 错误码映射（`-32004`/`-32005`/`-32600`）测试通过。

### Batch 9：WebServer（HTTP 主机 + 静态前端）
- **目标**：`http.Server` 生命周期、`embed.FS` 静态前端、CORS、`/healthz`、配置参数。
- **输入**：Batch 7-8 handlers/tools；`web/dist` 由前端构建产生。
- **输出**：`internal/webserver/{server,static,cors}.go` + 测试。
- **完成标准**：
  - `GET /healthz` 返回 ok。
  - `GET /` 返回 `index.html`（用占位 `web/dist` 跑通）。
  - 默认拒绝远端；`--host 0.0.0.0` 接受并打印 WARN。
  - `--port` 自定义生效且 GUI 菜单链接同步。

### Batch 10：前端骨架（Vite + React + 通信适配）
- **目标**：可构建的前端单代码库 + Wails/浏览器通信探测。
- **输入**：Batch 9 的 REST 契约（用作 fallback）。
- **输出**：`web/{package.json,vite.config.ts,tsconfig.json,index.html,src/{main.tsx,App.tsx,api/*.ts}}` + 通信层单测。
- **完成标准**：`pnpm test` + `pnpm build` 通过；`isWails()` 探测逻辑有测试。

### Batch 11：前端页面（CRUD + 出入库 + 流水 + 告急）
- **目标**：5 个页面 + Toast；Wails/浏览器同代码渲染。
- **输入**：Batch 10 骨架。
- **输出**：`web/src/pages/{AccessoryList,Inbound,Outbound,Flows,Replenishment}.tsx` + `components/Toast.tsx` + 组件测试。
- **完成标准**：5 个页面组件测试通过；Outbound 收到 409 时显示库存不足提示。

### Batch 12：桌面入口（Wails App + bindings + 菜单 + 单实例）
- **目标**：`wails.Run` 启动 GUI + bindings + 原生菜单 + 单实例锁。
- **输入**：Batch 9（webserver）+ Batch 11（前端构建产物）。
- **输出**：`app.go`、`main.go`、`internal/desktop/{bindings,menu,singleinstance}.go` + 测试。
- **完成标准**：
  - bindings 调用 service 路径有测试。
  - 第二次启动返回 `ErrAlreadyRunning` 且不创建新窗口。
  - 菜单"在浏览器中打开"使用当前配置的 host/port。

### Batch 13：集成与冒烟
- **目标**：e2e 双形态数据一致性 + 三平台 build 验证。
- **输入**：Batch 12。
- **输出**：`e2e_test.go`、`build/notes.md`。
- **完成标准**：
  - e2e：web 模式启动 → REST 创建配件 → MCP `accessory.get` 取到一致数据。
  - `go test ./...` 全绿。
  - `wails build` 在 macOS/Windows/Linux 三平台产物可启动（CI 或本机）。

## Test Obligations

- **必须先从失败测试开始的行为**（TDD Iron Law）：
  - 所有 service / handler / tool 方法的第一行测试代码必须先于实现存在并确认失败。
  - 失败信息必须具体（如 `expected accessories table, got: <error>`），不允许模糊断言。
  - 实现必须是最小化代码使测试通过；不允许引入未测试的额外功能。

- **必需的边界情况**（必须各自有测试）：
  - SKU 重复创建 → `ErrSKUConflict` / 409。
  - 配件删除时存在流水 → `ErrHasFlow` / 409。
  - `low_stock_threshold` 负值 → 400。
  - 批量入库任一行非法 → 整体回滚（库存与流水均不变）。
  - 批量出库任一行库存不足 → 整体回滚 + 第一条失败下标回显。
  - 出库刚好出完（`current_stock == quantity`）→ 成功且 `balance_after=0`。
  - 同 `client_ref` 二次提交入/出库 → 返回首次结果且不重复扣库存。
  - 阈值 0 配件不出现在告急列表；SKU 批次中存在未注册项 → `not_found` 字段回显。
  - MCP 错误码映射（`-32004`/`-32005`/`-32600`）。
  - webserver 默认拒绝远端；`--host 0.0.0.0` 接受并 WARN。

- **回归敏感区域**：
  - SQLite WAL/外键 pragma 设置（迁移前后都要）。
  - 事务边界（出入库 + 流水 MUST 同 tx）。
  - `embed.FS` 路径（前端构建产物路径变更会破坏打包）。
  - Wails bindings 导出名（前端依赖 `window.go.main.XService.*`）。
  - MCP 协议版本与 SDK 升级（升级前 read changelog）。

## Execution Mode

- **模式**：`Batch Inline`
- **选择理由**：
  - 体量中等（13 批），单批在子代理可承受范围；不必动用完整 SDD。
  - 但单批内部仍是 TDD 严格 5 步，不允许省略失败测试 → 最小实现 → 通过 → 提交。
  - Review Gates 在 Batch 4 / 8 / 12 触发强制 spec-compliance + code-quality 双视角审查。

## Verification Dimensions

| 维度 | 状态 | 发现 |
|------|------|------|
| Completeness | Pending | 36 条 SHALL/MUST 待逐条对照测试；6/13 批开始前 |
| Correctness | Pending | 事务回滚与幂等测试在 Batch 4 兑现 |
| Coherence | Pending | 双形态数据一致性 e2e 在 Batch 13 兑现 |

**总体结论**：Pending（待 DP-3 批准 + Batch 4/8/12 review gate 通过 + Batch 13 e2e 通过）

## Review Gates

- **强制审查点**：
  - **Batch 4（事务与幂等）**：必须由 spec-compliance + code-quality 双视角独立审查；任何"事务边界遗漏""幂等失效"判定为 Critical。
  - **Batch 8（MCP 错误映射）**：必须对照 `specs/mcp-server.md` 的 13 个 tool 与错误码表逐条核对。
  - **Batch 12（桌面入口与单实例）**：单实例锁与"激活已有窗口"路径必须有可观察证据。
- **阻塞类别**：
  - Critical：事务不原子、幂等失效、单实例不工作、MCP 协议不符、安全风险（默认远端访问）。
  - Important：错误响应未走统一结构、未配置 WAL、embed.FS 路径遗漏。
  - Minor：注释/命名/格式。

## Escalation Rules

- **何时回退到 `specifying`**：
  - 任何 capability 需要新增需求、修改 SHALL/MUST 语义、扩缩范围。
  - 设计决策需要变更（替换框架、调整端口策略、改存储后端等）。
  - 发现审批需求未被现有 spec 覆盖（"新 capability"）。

- **何时回退到 `bridging`**：
  - Batch 拆分/合并/重排序导致依赖图改变。
  - 任务粒度需要调整（5 步 TDD 阶段不适用）。
  - Review Gates 的强制审查点需新增。

- **何时不得继续实现**：
  - 任何测试 FAIL 且无法定位根因超过 2 次尝试 → 进入 `bug-investigator`。
  - 契约漂移（proposal 改了但 contract 未更新）→ 强制回 `bridging`。
  - DP-3 未批准 → 任何实现都禁止（hard gate）。

## 需求覆盖交叉校验

| Spec | Requirement 数 | 映射到 Batch | 测试覆盖 |
|---|---|---|---|
| accessory-catalog | 5 | Batch 3（service）+ Batch 7/8（REST/MCP） | Batch 3 单测 |
| stock-inbound | 4 | Batch 4 + Batch 7/8 | Batch 4 单测 |
| stock-outbound | 4 | Batch 4 + Batch 7/8 | Batch 4 单测 |
| inventory-flow | 4 | Batch 5 + Batch 7/8 | Batch 5 单测 |
| replenishment-advisor | 4 | Batch 6 + Batch 7/8 | Batch 6 单测 |
| mcp-server | 4 | Batch 8 | Batch 8 单测 |
| desktop | 5 | Batch 12 | Batch 12 单测 |
| web-server | 6 | Batch 9 | Batch 9 单测 |

**未映射需求**：无（36/36 全部分配）。
**模糊点**：无（每个 spec 的 Scenario 在测试中均有可观察断言）。