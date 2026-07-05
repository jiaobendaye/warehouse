# 变更提案：手机配件管理系统

## 背景（Why）

- 当前仓库为空，需要从零搭建一个面向小型门店/个人工作者的手机配件进销存工具，替代手工记账与电子表格。
- 配件（保护壳、贴膜、数据线、充电器、电池等）品类多、单品价值低，需要快速完成单笔与批量出入库，并自动留下流水以便事后追溯。
- 经营场景中"哪些配件快卖完了需要补货"是高频决策，需要系统基于当前库存与告急阈值给出明确建议。
- 桌面应用形态便于离线使用；通过内嵌 Web 简化前端开发；通过 MCP 让外部 AI 代理也能查询/操作库存（例如自动统计、对账、问答）。

## 变更内容（What Changes）

- 引入 Wails（Go + 前端 WebView）作为桌面应用骨架，单一二进制可在 Windows / macOS / Linux 运行。
- **同一应用进程同时提供两种入口**：
  - **GUI 形态**：Wails 启动原生桌面窗口，前端代码（React/Vite 或 Svelte）以 WebView 形式渲染，使用 Wails bindings 与 Go 后端通信。
  - **Web 形态**：同一份前端被构建为静态资源后，由进程内置的 HTTP 服务对外提供；同时该 HTTP 服务暴露 REST API 与 MCP（HTTP/SSE 或 streamable）端点，浏览器（同机或局域网内的其它设备）通过 `http://<host>:<port>` 访问。
- 两个形态共享同一份 Go 后端业务逻辑与同一份 SQLite 数据库；前端代码 100% 复用，仅在通信层区分：
  - GUI 形态 → 走 Wails IPC（类型绑定方法）。
  - Web 形态 → 走 REST + MCP over HTTP/SSE。
- 默认绑定 `127.0.0.1:17880`（端口与监听地址可在启动参数或设置中调整）；如需局域网访问由用户在启动时显式开启 `--host 0.0.0.0`。
- 核心业务能力：
  - 配件 CRUD（型号、名称、单位、SKU、库存阈值）
  - 入库（单条 + 批量）：更新库存、追加入库流水
  - 出库（单条 + 批量）：更新库存、追加出库流水、库存不足时拒绝
  - 库存查询与流水查询（按时间范围、配件筛选）
  - 告急/补货建议：基于"当前库存 < 阈值"或"按指定 SKU 列表判断哪些需要补"
- 本地存储：SQLite（嵌入式，零运维）。
- 不引入用户体系与权限控制，所有操作视为同一管理员。

## 能力（Capabilities）

### 新增能力

- accessory-catalog：配件目录管理（增删改查、阈值设置）。
- stock-inbound：配件入库（单条与批量），写入流水。
- stock-outbound：配件出库（单条与批量），写入流水，库存校验。
- inventory-flow：出入库流水查询与导出。
- replenishment-advisor：基于当前库存与阈值的告急/补货判断。
- mcp-server：对外暴露上述能力的 MCP 工具集（stdio + HTTP/SSE 两种传输，由 Web 形态的 HTTP 端口承载）。
- desktop：Wails 桌面应用壳（原生窗口 + WebView GUI 入口）。
- web-server：进程内 HTTP 服务，承载前端静态资源、REST API 与 MCP 端点，浏览器可访问。

### 修改能力

- 无（项目从零起步，无既有能力需要修改）。

## 范围（Scope）

### 范围内（In Scope）

- Wails 项目脚手架与构建配置（同时生成桌面 GUI 入口与嵌入式 Web 入口）。
- Go 后端领域模型、仓储层、业务服务层、Wails bindings、REST API、MCP Server。
- 前端单代码库：同时适配 Wails WebView 与浏览器（同构页面 + 通信层抽象）。
- HTTP 服务：端口/绑定地址可配置；提供 `/` 静态前端、`/api/*` REST、`/mcp` MCP 端点。
- SQLite schema 与初始迁移。
- MCP 工具清单（list/get/create/update/delete + inbound/outbound/flow query + replenishment check），同时支持 stdio 与 HTTP/SSE 传输。
- 基础本地运行说明（README 启动命令，包括 GUI 启动、Web 启动、远程访问开关）。

### 范围外（Out of Scope）

- 账号、登录、权限与多租户。
- 审计日志、消息推送、移动端 App。
- 报表导出（CSV/PDF 暂时不在 v1）。
- 远程同步、云端备份、电商平台对接。
- 复杂的条码/扫码硬件集成（仅留接口位）。

## 影响（Impact）

- 影响的代码区域：仓库初始目录将出现 `wails/`（或等价）项目结构；Go 与前端代码同仓库共存。
- 影响的 API 或接口：
  - 内部 Go service → 前端 IPC（Wails bindings）
  - 内部 Go service → MCP（stdio / 本地 socket）
  - 内部 Go service → SQLite（Wails 默认路径下的本地文件）
- 依赖或涉及的外部系统：
  - Wails v2/v3 CLI
  - Go modules
  - Node + 前端框架
  - SQLite 驱动（`modernc.org/sqlite` 纯 Go 实现，避免 CGO）
  - MCP SDK（`github.com/modelcontextprotocol/go-sdk` 或等价）
  - HTTP 路由（`net/http` + `chi` 或等价）
  - 前端通信适配层：检测是否运行在 Wails 环境（`window.go.*` / `window.runtime.*`），否则走 `fetch(/api/...)`