# 桌面入口（desktop）

## ADDED Requirements

### Requirement: 启动原生窗口

The system MUST 在以桌面模式启动时打开一个原生桌面窗口，加载前端 GUI 入口并以 WebView 渲染。

#### Scenario: GUI 启动成功

- **WHEN** 应用以默认（无 `--web-only` / `--mcp-stdio`）参数启动
- **THEN** 系统 MUST 创建主窗口、加载前端、并在窗口准备好后保留 GUI 进程

#### Scenario: 窗口大小

- **WHEN** 主窗口创建时
- **THEN** 默认窗口尺寸 MUST 为 `1280×800`，且 MUST 允许用户手动调整

### Requirement: GUI 通信走 Wails IPC

GUI 形态下，前端 MUST 通过 Wails bindings 调用 Go 后端；禁止前端直接 fetch `/api/*`。

#### Scenario: 前端调用 Go 方法

- **WHEN** 前端调用 `AccessoryService.List({ q: "壳" })`
- **THEN** 系统 MUST 通过 Wails IPC 路由到 Go 端 `AccessoryService.List` 并返回结果

### Requirement: 菜单与基本交互

桌面形态 MUST 提供原生菜单，至少包含：打开主窗口、打开 Web 形态（拉起系统默认浏览器到 `http://127.0.0.1:<port>`）、退出。

#### Scenario: 菜单打开 Web

- **WHEN** 用户在原生菜单点击"在浏览器中打开"
- **THEN** 系统 MUST 用系统默认浏览器打开 `http://127.0.0.1:<configured-port>/`

### Requirement: 单实例

桌面形态 MUST 保证单实例运行：第二次启动 MUST 激活已有窗口而非开新窗口。

#### Scenario: 重复启动

- **WHEN** 应用已经在运行，用户再次双击图标
- **THEN** 已存在的主窗口 MUST 被前置显示，且不创建新进程窗口

### Requirement: GUI 与 Web 同源

GUI 形态的 WebView 与浏览器形态访问的 `/` 前端 MUST 使用同一份构建产物；窗口与浏览器展示一致。

#### Scenario: 一致性

- **WHEN** 前端代码发生变更并重新构建
- **THEN** 桌面 GUI 与浏览器访问 MUST 同步更新（无需在两端分别维护）