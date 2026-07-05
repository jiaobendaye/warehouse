# 补货建议（replenishment-advisor）

## ADDED Requirements

### Requirement: 全量告急扫描

The system MUST 支持扫描全部配件，返回所有 `current_stock < low_stock_threshold` 的配件，按缺口（`threshold - current_stock`）倒序。

#### Scenario: 存在告急配件

- **WHEN** 调用方不带参数请求告急列表
- **THEN** 系统 MUST 返回所有 `low_stock_threshold > 0` 且 `current_stock < low_stock_threshold` 的配件，附带 `shortage = threshold - current_stock`

#### Scenario: 无告急

- **WHEN** 没有配件满足告急条件
- **THEN** 系统 MUST 返回空数组（`200 OK`）

### Requirement: 给定批次判断

The system MUST 支持调用方提供一组 `sku` 或 `accessory_id`，返回这些配件中需要补货的子集与建议补货数量。

#### Scenario: 部分告急

- **WHEN** 调用方提供 `["sku-A", "sku-B", "sku-C"]`，其中仅 A、C 告急
- **THEN** 系统 MUST 仅返回 A、C，并标注 `shortage` 与建议补货数（默认等于 `shortage`）

#### Scenario: 含不存在项

- **WHEN** 调用方提供的 SKU 列表中存在未注册的 SKU
- **THEN** 系统 MUST 在响应中以 `not_found: [...]` 字段列出未找到的 SKU，且不影响其他项的判断

### Requirement: 阈值 0 不参与告急

`low_stock_threshold=0` 的配件 MUST 永远不出现在告急列表中。

#### Scenario: 阈值为 0 的配件不告急

- **WHEN** 配件 `current_stock=0`、`low_stock_threshold=0`
- **THEN** 全量告急扫描 MUST 不包含该配件

### Requirement: 建议补货数量策略

v1 MUST 默认建议补货数 = `shortage = threshold - current_stock`；调用方可在请求中传入 `policy=fixed:<N>` 指定固定补货数（覆盖默认）。

#### Scenario: 默认策略

- **WHEN** 调用方不传 `policy`
- **THEN** `suggested_quantity` MUST 等于 `shortage`

#### Scenario: fixed 策略

- **WHEN** 调用方传 `policy=fixed:50`
- **THEN** `suggested_quantity` MUST 为 `50`（即使 `shortage=10` 也按 50 给出）