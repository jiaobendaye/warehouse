# Warehouse — 手机配件管理系统

Wails（Go + WebView）+ 嵌入式 Web + MCP 的桌面应用，覆盖配件目录、出入库、流水、告急判断。

## 启动

```bash
# GUI 模式（需 Wails SDK）
make run-gui

# 命令行模式（无需 Wails，HTTP + MCP 自动启动）
make run
# → http://127.0.0.1:17880 （前端 + REST API + MCP）
```

## 常用参数

| 参数 | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--host` | `WAREHOUSE_HOST` | `127.0.0.1` | HTTP 监听地址；`0.0.0.0` 开启远程 |
| `--port` | `WAREHOUSE_PORT` | `17880` | HTTP 监听端口 |
| `--db` | `WAREHOUSE_DB` | `~/.warehouse/data.db` | SQLite 数据库路径 |

## 构建

```bash
make build       # 命令行模式
make build-gui   # GUI 模式（需 Wails SDK）
make web-build   # 仅前端
```

## 测试

```bash
make test        # 单元测试
make test-e2e    # e2e 集成测试
make test-all    # 全部
```

## 项目结构

```
├── main.go                  # 入口
├── app_wails.go             # Wails GUI（build tag: wails）
├── app_noop.go              # 命令行 fallback
├── internal/
│   ├── config/              # flag/env 解析
│   ├── db/                  # SQLite + migration
│   ├── domain/              # Accessory, InventoryFlow
│   ├── repo/                # 仓储层
│   ├── service/             # 业务逻辑
│   ├── api/                 # REST (chi)
│   ├── mcp/                 # MCP (13 tools, stdio + HTTP)
│   ├── webserver/           # HTTP 服务 + embed 前端
│   └── desktop/             # Wails bindings + 菜单 + 单实例 + ServerManager
├── web/                     # React + Vite + TS 前端
└── changes/                 # spec-superflow 产物
```