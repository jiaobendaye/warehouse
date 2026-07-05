# 技术设计：手机配件管理系统

## 上下文

- **当前状态**：仓库为空（仅 `.git`），尚未引入任何语言、框架或目录约定。
- **约束条件**：
  - 必须使用 Wails 作为桌面应用骨架（Go + 前端 WebView）。
  - 同一进程必须同时支持原生桌面窗口与本地 HTTP 服务（双形态入口）。
  - 必须支持 MCP（stdio + HTTP/SSE）。
  - 本地存储，不引入外部数据库。
  - v1 不做账号、权限、审计日志、远程同步。
  - DP-0 已确认：`change=mobile-accessories-management`，模式 `full`，聚焦核心库存能力。
- **利益相关者**：
  - 店主/库管（GUI 与浏览器形态的主要使用者）。
  - 外部 AI 代理/工具（MCP 客户端）。
  - 局域网内的浏览器用户（HTTP 形态）。

## 目标

- 提供可在本机离线运行的桌面应用，并通过同一二进制支持浏览器远程访问。
- 实现"配件目录 + 出入库 + 流水 + 告急判断"的完整闭环。
- 通过 MCP 暴露所有能力，便于 AI 代理集成。
- 业务实现遵循 TDD Iron Law：先失败测试，再实现。
- 所有能力必须有可测试的 Scenario 对应。

## 非目标

- 用户体系、登录、权限、多租户。
- 审计日志、消息推送、移动端 App。
- 报表导出（CSV/PDF）、图表可视化。
- 远程数据库、云端备份、电商平台对接。
- 复杂条码/扫码硬件集成（仅留接口位）。
- 多语言 i18n（v1 仅中文 UI）。

## 决策

### 决策 1：进程内"双形态"模式

- **选择**：Wails 在 `wails.Run` 启动 GUI 的同时，由 Go 进程内置 `http.Server` 监听本地端口；前端代码 100% 复用，仅在前端封装通信层（探测是否在 Wails 环境，否则走 `fetch /api/v1`）。
- **理由**：
  - 满足"同一应用支持 GUI 和 Web"的明确诉求。
  - 后端逻辑只写一次：业务服务（`AccessoryService`/`StockService`/`FlowService`/`ReplenishmentService`）被 Wails bindings 与 REST handler 共享。
  - 部署形态唯一：用户只跑一个二进制。
- **考虑的替代方案**：
  - *两个独立二进制*（一个 Wails GUI、一个独立 Web 服务）：部署复杂、需共享数据库路径且双进程同步易出问题，否决。
  - *只跑 Web，浏览器即一切*：放弃原生体验，与 Wails 选型冲突，否决。
  - *Wails Remote*（v3 实验功能）：生态尚不稳定，否决。

### 决策 2：本地存储用 SQLite（pure-Go 驱动）

- **选择**：使用 `modernc.org/sqlite`（纯 Go 编译，无 CGO），数据库文件位于 `~/.warehouse/data.db`（可在启动参数覆盖）。
- **理由**：
  - 桌面应用零运维，SQLite 嵌入式最简。
  - 纯 Go 驱动避免 CGO 交叉编译痛苦，对 Wails 多平台构建友好。
  - 事务能力满足"批量出入库整体回滚"硬需求。
- **考虑的替代方案**：
  - *BoltDB / BadgerDB*：KV 模型，需自行实现关系查询与索引，复杂度高，否决。
  - *PostgreSQL / MySQL*：引入外部依赖，与"本地存储"诉求冲突，否决。
  - *纯 JSON 文件*：并发与原子性差，否决。

### 决策 3：MCP SDK 选型

- **选择**：`github.com/modelcontextprotocol/go-sdk`（官方 Go SDK）。
- **理由**：官方维护，与协议同步；天然支持 stdio 与 streamable HTTP 传输；类型绑定干净。
- **考虑的替代方案**：
  - *自实现 MCP*：协议复杂度高、易出错，否决。
  - *社区三方 SDK*（如 `metoro-io/mcp-go`）：生态活跃但非官方，关键 bug 修复滞后，备用。

### 决策 4：HTTP 路由与 REST 形态

- **选择**：使用 `net/http` + `github.com/go-chi/chi/v5`（轻量、与 stdlib 友好）；路径前缀 `/api/v1`；统一错误响应 `{ "error": { "code", "message" } }`。
- **理由**：
  - chi 中间件链对 CORS、请求日志、recover 友好。
  - 不引入更重的框架（gin/echo）以减小二进制体积。
  - 错误响应统一格式便于前端与 MCP 客户端解析。
- **考虑的替代方案**：
  - *Gin*：性能略好但绑定较重，否决。
  - *仅 stdlib + 自写 mux*：功能足够但 CORS/中间件都得手写，不经济。

### 决策 5：前端框架与通信适配

- **选择**：React 18 + Vite + TypeScript；通信层封装 `apiClient`，启动时探测 `window.runtime`（Wails 提供），存在则走 Wails bindings，否则走 `fetch(/api/v1)`。
- **理由**：
  - React 生态成熟，组件复用度高。
  - Vite 构建快，产物可直接被 Go `embed.FS` 打包进二进制。
  - Wails v2 已支持 `embed.FS` 注入前端构建产物。
- **考虑的替代方案**：
  - *Svelte/SvelteKit*：更轻量但生态较小，否决（可在 v2 评估）。
  - *Vue 3*：可行但团队熟悉度按 React 优先。
  - *纯 HTML + Alpine*：开发体验差、对复杂表单不友好，否决。

### 决策 6：Wails 项目布局

- **选择**：
  ```
  repo root
  ├─ app.go              # Wails App struct + OnStartup
  ├─ main.go             # 入口：解析 flag，分发到 GUI / web-only / mcp-stdio
  ├─ internal/
  │  ├─ service/         # 业务服务（GUI/REST/MCP 共享）
  │  ├─ repo/            # SQLite 仓储
  │  ├─ api/             # REST handlers
  │  ├─ mcp/             # MCP server + tool 注册
  │  └─ desktop/        # Wails bindings 适配
  ├─ web/                # 前端（Vite）
  └─ migrations/         # SQL DDL
  ```
- **理由**：与 `internal/` 标准布局对齐，业务与传输层清晰分离；`embed.FS` 直接指向 `web/dist`。
- **考虑的替代方案**：
  - *按 capability 切目录*（DDD 风格）：v1 体量小，过度设计，否决。
  - *全部放 `pkg/`*：暴露内部 API 给外部不利于演进，否决。

### 决策 7：单实例与端口策略

- **选择**：
  - 单实例：使用 `github.com/boltdb/bolt` 的文件锁方式（独立进程级），或平台特定的 `flock`；Wails v2 自身会处理 macOS/Windows 单实例，Linux 通过 `~/.warehouse.lock` 实现。
  - 默认监听 `127.0.0.1:17880`；远程访问显式 `--host 0.0.0.0` 开启并打 WARN。
- **理由**：默认安全（仅本机），远程访问需用户明确意图，避免被网络邻居意外访问。

## 风险与权衡

- **风险 1**：Wails v2 与 React/Vite 版本兼容性问题（特别是 HMR 与 WebView2 交互）。
  - **缓解**：固定 Wails CLI 与前端依赖版本；CI 在三个平台跑 smoke build。
- **风险 2**：SQLite 在高并发写入下锁竞争（WAL 模式可缓解）。
  - **缓解**：默认开启 WAL；批量出入库走单事务，减少锁次数。
- **风险 3**：同一进程同时跑 GUI 与 HTTP，若任一 panic 影响另一个。
  - **缓解**：HTTP server 与 GUI 各自独立 goroutine + recover 中间件；GUI 主线程异常仅退出窗口不杀进程。
- **风险 4**：MCP 协议升级导致 SDK 不兼容。
  - **缓解**：固定 SDK 版本在 go.mod；升级前读 changelog 与协议草案。
- **风险 5**：远程访问开启后无鉴权，被局域网任意访问。
  - **缓解**：v1 默认仅 127.0.0.1；启动时 `--host 0.0.0.0` 显式开启并 WARN；v2 再考虑 token。
- **风险 6**：前端构建产物 `embed.FS` 体积影响二进制大小。
  - **缓解**：前端代码分割；使用 `//go:embed all:web/dist` 仅打包产物不打包源码。
- **权衡**：为了 v1 快速交付，舍弃用户体系与审计日志；未来扩展需回到 `specifying` 状态追加 capability。

## 迁移计划

> 本项目为新建项目，无既有部署。"迁移计划"在此等价于"上线步骤"与"回滚步骤"。

- **上线步骤**：
  1. 在开发机 `wails dev` 跑通 GUI + Web + MCP 三形态。
  2. `wails build` 生成三个平台产物（macOS / Windows / Linux）。
  3. 在用户机器首次启动时自动创建 `~/.warehouse/` 目录与 SQLite 文件，执行 migration。
  4. 通过 README 告知启动方式与默认端口。
- **回滚步骤**：
  - v1 期间数据库 schema 通过 `migrations/0001_init.sql` 起步，单一文件；任何 schema 变更必须新增 `000X_xxx.sql` 文件并保证向后兼容；破坏性变更需走 `specifying` 状态重新走流程。
  - 客户端回滚即替换二进制；老版本 schema 必须能被新版本向下读取。

## 待明确问题

- 问题 1：是否需要在 v1 提供"按批次/批号"维度？（用户未提，倾向不做）
  - 决策负责人：用户 / 项目负责人。
- 问题 2：MCP 工具命名是否需要在工具名前加命名空间（如 `warehouse.accessory.list`）？
  - 决策负责人：MCP 客户端生态调研后定，v1 暂不加。
- 问题 3：默认端口 17880 是否与其他常见工具冲突？如冲突是否改端口？
  - 决策负责人：发布前做端口冲突检查。