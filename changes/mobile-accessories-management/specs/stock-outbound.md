# 出库（stock-outbound）

## ADDED Requirements

### Requirement: 单条出库

The system MUST 允许对单个配件执行出库操作，必须提供 `accessory_id`、`quantity`（正整数）、`unit_price`（非负数，可选，默认 0）、`remark`（可空）、`occurred_at`（可空，默认当前时间）。

#### Scenario: 出库成功

- **WHEN** 配件存在且 `quantity>=1` 且 `current_stock >= quantity`
- **THEN** 系统 MUST 在同一事务内：① `accessories.current_stock -= quantity` ② 在 `inventory_flow` 追加一条 `type='out'` 的流水（含 `balance_after`）③ 返回更新后的库存与流水

#### Scenario: 库存不足被拒

- **WHEN** `current_stock < quantity`
- **THEN** 系统 MUST 返回 `409 Conflict` 与 `code=INSUFFICIENT_STOCK`，且不修改任何数据

#### Scenario: 配件不存在

- **WHEN** `accessory_id` 不存在
- **THEN** 系统 MUST 返回 `404 Not Found` 且不修改任何数据

### Requirement: 批量出库

The system MUST 允许一次提交多条出库明细（`items[]`），每条独立校验，全部成功或全部回滚。

#### Scenario: 全部成功

- **WHEN** `items` 中每行均通过存在性与库存校验
- **THEN** 系统 MUST 在同一事务内依次扣减库存并写入 `type='out'` 流水，返回 `accepted` 数量与流水 ID 列表

#### Scenario: 任一库存不足整体回滚

- **WHEN** `items` 中至少一行库存不足
- **THEN** 系统 MUST 整体回滚（库存与流水均不变），并返回 `409 Conflict` 与第一条失败的明细下标与原因

### Requirement: 出库流水记录

每条出库 MUST 生成一条 `inventory_flow` 记录，字段包含 `accessory_id`、`type='out'`、`quantity`、`unit_price`、`balance_after`、`remark`、`occurred_at`、`created_at`。

#### Scenario: balance_after 正确

- **WHEN** 出库前 `current_stock=10`，本次 `quantity=3`
- **THEN** 流水记录的 `balance_after` MUST 为 `7`

#### Scenario: 允许当前库存等于阈值

- **WHEN** `current_stock == quantity`（刚好出完）
- **THEN** 出库 MUST 成功，`balance_after=0`

### Requirement: 出库接口幂等性

出库接口 MUST 支持可选 `client_ref`，语义与入库一致：同一 `client_ref` 重复提交 MUST 返回首次结果且不重复扣库存。

#### Scenario: 同 client_ref 重复

- **WHEN** 调用方用同一 `client_ref` 提交第二次出库
- **THEN** 系统 MUST 返回第一次的流水记录（status 200）且不重复扣库存