# 配件目录（accessory-catalog）

## ADDED Requirements

### Requirement: 创建配件

The system MUST 允许创建一个新配件，字段至少包含 `sku`（唯一）、`name`、`unit`、`low_stock_threshold`（非负整数）、`notes`（可空）。

#### Scenario: 创建成功

- **WHEN** 调用方提供合法的 `sku`（未占用）、`name`、`unit` 与 `low_stock_threshold`
- **THEN** 系统在 `accessories` 表插入一行并返回完整记录（包含生成的 `id` 与创建时间）

#### Scenario: SKU 重复被拒

- **WHEN** 调用方提供的 `sku` 已存在
- **THEN** 系统 MUST 返回 `409 Conflict` 与明确错误信息，且不写入任何行

#### Scenario: 字段缺失被拒

- **WHEN** `sku`、`name`、`unit` 中任意一个缺失或为空
- **THEN** 系统 MUST 返回 `400 Bad Request` 与明确错误信息

### Requirement: 查询配件

The system MUST 支持按 `id`、`sku` 或筛选条件（关键字匹配 `name`/`sku`）查询配件，并支持分页（`limit`、`offset`）。

#### Scenario: 列表查询

- **WHEN** 调用方不带筛选条件查询配件列表
- **THEN** 系统 MUST 返回配件列表（按 `created_at` 倒序），以及 `total` 总数与 `limit`/`offset` 回显

#### Scenario: 关键字筛选

- **WHEN** 调用方提供 `q=壳` 关键字
- **THEN** 系统 MUST 返回 `name` 或 `sku` 包含该子串（大小写不敏感）的配件

### Requirement: 更新配件

The system MUST 允许更新配件的 `name`、`unit`、`low_stock_threshold`、`notes`；`sku` 一经创建不可修改。

#### Scenario: 更新成功

- **WHEN** 调用方提供存在的 `id` 与合法字段
- **THEN** 系统 MUST 更新该行并返回更新后的完整记录

#### Scenario: 修改 SKU 被拒

- **WHEN** 调用方尝试修改 `sku`
- **THEN** 系统 MUST 返回 `400 Bad Request`，且不修改任何字段

### Requirement: 删除配件

The system MUST 允许删除配件，但 MUST 在该配件存在流水记录时拒绝删除并返回 `409 Conflict`。

#### Scenario: 无流水时删除成功

- **WHEN** 配件没有任何流水记录
- **THEN** 系统 MUST 删除该行并返回 `204 No Content`

#### Scenario: 存在流水时删除被拒

- **WHEN** 配件在 `inventory_flow` 表中存在至少一条流水
- **THEN** 系统 MUST 返回 `409 Conflict` 与提示"该配件存在流水记录，禁止删除"

### Requirement: 库存阈值字段语义

`low_stock_threshold` MUST 为非负整数；`0` 表示不参与告急判断；任何负值 MUST 被拒绝。

#### Scenario: 阈值为 0

- **WHEN** 创建或更新配件时 `low_stock_threshold=0`
- **THEN** 系统 MUST 接受并将该配件标记为"不参与告急"

#### Scenario: 阈值为负

- **WHEN** 创建或更新配件时 `low_stock_threshold<0`
- **THEN** 系统 MUST 返回 `400 Bad Request`