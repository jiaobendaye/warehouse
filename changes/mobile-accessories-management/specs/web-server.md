# Web 服务（web-server）

## ADDED Requirements

### Requirement: 进程内 HTTP 服务

The system MUST 在同一进程中启动一个 HTTP 服务，承担三种端点：① 静态前端 ② REST API ③ MCP 端点。

#### Scenario: 同时启动

- **WHEN** 应用以默认参数启动
- **THEN** HTTP 服务 MUST 监听 `127.0.0.1:17880`，并响应 `/`、`/api/*`、`/mcp`

### Requirement: 静态前端托管

`GET /` MUST 返回前端入口 HTML；`GET /assets/*` MUST 返回打包后的静态资源；MIME 类型正确。

#### Scenario: 根路径

- **WHEN** 浏览器请求 `GET /`
- **THEN** 系统 MUST 返回 `index.html` 且 `Content-Type: text/html; charset=utf-8`

#### Scenario: 静态资源

- **WHEN** 浏览器请求 `GET /assets/main-<hash>.js`
- **THEN** 系统 MUST 返回对应文件且 `Content-Type` 匹配

### Requirement: REST API 契约

REST API MUST 镜像 MCP 与 GUI bindings 的能力，路径前缀 `/api/v1`，所有请求/响应使用 JSON。

#### Scenario: 路径前缀

- **WHEN** 浏览器请求 `GET /api/v1/accessories`
- **THEN** 系统 MUST 返回配件列表 JSON

#### Scenario: 错误响应统一

- **WHEN** 任何 `/api/*` 端点发生错误
- **THEN** 响应 MUST 为 `{ "error": { "code": "...", "message": "..." } }` 且 HTTP 状态码正确（400/404/409/500）

### Requirement: CORS

为便于浏览器同源访问，REST/MCP 端点 MUST 仅允许同源（默认）；若用户开启远程访问，必须允许 `Origin: <configured-allowed-origin>`。

#### Scenario: 同源允许

- **WHEN** 浏览器从 `http://127.0.0.1:17880` 调用 `/api/v1/accessories`
- **THEN** 系统 MUST 正常响应且不返回 CORS 错误

#### Scenario: 跨域拒绝

- **WHEN** 浏览器从 `http://evil.example` 调用同端点
- **THEN** 系统 MUST 在未显式允许的情况下返回 CORS 拒绝

### Requirement: 配置项

HTTP 服务的 host/port MUST 可通过命令行参数或环境变量覆盖：`--host`、`--port` 或 `WAREHOUSE_HOST`、`WAREHOUSE_PORT`。

#### Scenario: 自定义端口

- **WHEN** 应用以 `--port 19090` 启动
- **THEN** 系统 MUST 监听 `127.0.0.1:19090`，且 GUI 菜单中"在浏览器中打开"链接 MUST 同步更新

#### Scenario: 远程访问显式开启

- **WHEN** 应用以 `--host 0.0.0.0` 启动
- **THEN** 系统 MUST 在所有接口监听，并打印一条 WARN 日志提醒已开启远程访问

### Requirement: 健康检查

The system MUST 提供 `GET /healthz` 端点，返回 `{ "status": "ok", "version": "..." }`。

#### Scenario: 健康检查

- **WHEN** 任何客户端请求 `/healthz`
- **THEN** 系统 MUST 返回 `200 OK` 与状态 JSON