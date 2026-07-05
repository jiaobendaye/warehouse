# 入库（stock-inbound）

## ADDED Requirements

### Requirement: 单条入库

The system MUST 允许对单个配件执行入库操作，必须提供 `accessory_id`、`quantity`（正整数）、`unit_cost`（非负数，可选，默认 0）、`remark`（可空）、`occurred_at`（可空，默认当前时间）。

#### Scenario: 入库成功

- **WHEN** 调用方提供合法的 `accessory_id` 与 `quantity>=1`
- **THEN** 系统 MUST 在同一事务内：① `accessories.current_stock += quantity` ② 在 `inventory_flow` 追加一条 `type='in'` 的流水 ③ 返回更新后的库存与流水记录

#### Scenario: 配件不存在

- **WHEN** `accessory_id` 不存在
- **THEN** 系统 MUST 返回 `404 Not Found` 且不修改任何数据

#### Scenario: 数量非法

- **WHEN** `quantity<=0` 或非整数
- **THEN** 系统 MUST 返回 `400 Bad Request` 且不修改任何数据

### Requirement: 批量入库

The system MUST 允许一次提交多条入库明细（`items[]`），全部成功或全部回滚。

#### Scenario: 全部成功

- **WHEN** 调用方提交 `items` 数组，所有行的 `accessory_id` 合法且 `quantity>=1`
- **THEN** 系统 MUST 在同一事务内依次更新库存与写入流水，并返回 `accepted` 数量与流水 ID 列表

#### Scenario: 部分失败回滚

- **WHEN** `items` 中存在至少一条非法记录（如 `accessory_id` 不存在或 `quantity<=0`）
- **THEN** 系统 MUST 整体回滚（不修改任何 `accessories.current_stock`，不写入任何 `inventory_flow`），并返回 `400 Bad Request` 与失败明细下标

### Requirement: 入库流水记录

每条入库 MUST 生成一条 `inventory_flow` 记录，字段包含 `accessory_id`、`type='in'`、`quantity`、`unit_cost`、`balance_after`（本次入库后的库存）、`remark`、`occurred_at`、`created_at`。

#### Scenario: balance_after 正确

- **WHEN** 入库前 `current_stock=10`，本次 `quantity=5`
- **THEN** 流水记录的 `balance_after` MUST 为 `15`

#### Scenario: occurred_at 可回填

- **WHEN** 调用方显式提供 `occurred_at`（RFC3339 字符串）
- **THEN** 流水 MUST 使用该时间；若未提供 MUST 使用服务端当前时间

### Requirement: 入库接口幂等性

入库接口 MUST 支持可选 `client_ref`（调用方提供的幂等键）；若同一 `client_ref` 已存在对应流水 MUST 拒绝再次提交并返回已存在的流水。

#### Scenario: 同 client_ref 重复

- **WHEN** 调用方用同一 `client_ref` 提交第二次入库
- **THEN** 系统 MUST 返回第一次的流水记录（status 200）且不重复扣库存

#### Scenario: client_ref 全局唯一

- **WHEN** `client_ref` 与历史任一入库流水的 `client_ref` 冲突
- **THEN** 系统 MUST 视为重复请求并按上一场景处理