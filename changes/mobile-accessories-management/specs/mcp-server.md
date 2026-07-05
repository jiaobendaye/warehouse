# MCP 服务（mcp-server）

## ADDED Requirements

### Requirement: 工具清单

The system MUST 作为 MCP server 暴露以下工具（tool name + 描述），覆盖配件管理的所有读/写能力：

| Tool | 说明 |
|---|---|
| `accessory.list` | 列出配件，支持关键字 `q`、分页 |
| `accessory.get` | 按 `id` 或 `sku` 取单个配件 |
| `accessory.create` | 创建配件 |
| `accessory.update` | 更新配件（除 `sku` 外） |
| `accessory.delete` | 删除配件（无流水时） |
| `stock.inbound` | 单条入库，支持 `client_ref` 幂等 |
| `stock.outbound` | 单条出库，支持 `client_ref` 幂等 |
| `stock.batch_inbound` | 批量入库（整体事务） |
| `stock.batch_outbound` | 批量出库（整体事务） |
| `flow.list` | 流水查询（按配件/时间/类型/分页） |
| `flow.get` | 单条流水详情 |
| `replenishment.scan` | 全量告急扫描 |
| `replenishment.check` | 给定 SKU 批次判断 |

#### Scenario: 工具可被发现

- **WHEN** MCP 客户端发起 `tools/list`
- **THEN** 系统 MUST 返回上述工具的 JSON Schema 描述

#### Scenario: 调用工具

- **WHEN** MCP 客户端发起 `tools/call` 并提供合法参数
- **THEN** 系统 MUST 调用对应的内部服务方法并以 MCP 内容（JSON）形式返回结果

### Requirement: 传输方式

The system MUST 同时支持两种 MCP 传输：
- **stdio**：直接进程 stdio 通信，便于本地 CLI/AI 代理作为子进程拉起。
- **HTTP/SSE**（或 streamable HTTP）：通过 `web-server` 的 HTTP 端口承载 `/mcp` 端点。

#### Scenario: stdio 启动

- **WHEN** 应用以 `--mcp-stdio` 启动（或同等参数）
- **THEN** 系统 MUST 不启动 GUI 与 HTTP 服务，仅暴露 stdio MCP server，并禁用一切 stdout 日志（避免污染协议）

#### Scenario: HTTP/SSE 端点

- **WHEN** `web-server` 已启动
- **THEN** `GET /mcp` MUST 作为 streamable HTTP 端点接受 MCP 请求

### Requirement: 错误映射

MCP 工具调用 MUST 将内部错误码映射为 MCP 错误：`NOT_FOUND` → `code=-32004`、`CONFLICT` → `-32005`、`BAD_REQUEST` → `-32600`。

#### Scenario: 配件不存在

- **WHEN** 客户端调用 `accessory.get` 但 `id` 不存在
- **THEN** 系统 MUST 返回 MCP 错误 `code=-32004`，`message` 包含 "accessory not found"

#### Scenario: 库存不足

- **WHEN** 客户端调用 `stock.outbound` 但库存不足
- **THEN** 系统 MUST 返回 MCP 错误 `code=-32005`，`message` 包含 "INSUFFICIENT_STOCK"

### Requirement: 鉴权

v1 不提供鉴权；MCP 端点 MUST 仅绑定 `127.0.0.1` 或用户显式开启的接口；远程访问默认拒绝。

#### Scenario: 默认仅本机

- **WHEN** 应用以默认配置启动
- **THEN** MCP 端点 MUST 仅对 `127.0.0.1` 可达

#### Scenario: 显式开启远程

- **WHEN** 应用以 `--host 0.0.0.0` 启动
- **THEN** MCP 端点 MUST 监听在所有接口