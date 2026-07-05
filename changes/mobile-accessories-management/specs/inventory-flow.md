# 库存流水（inventory-flow）

## ADDED Requirements

### Requirement: 按配件查询流水

The system MUST 支持按 `accessory_id` 查询流水记录，支持按时间范围（`from`、`to`，RFC3339）筛选，支持按 `type`（`in`/`out`/全部）筛选，并支持分页。

#### Scenario: 按配件 + 类型筛选

- **WHEN** 调用方提供 `accessory_id=42`、`type='in'`、`from=2026-01-01T00:00:00Z`
- **THEN** 系统 MUST 仅返回该配件在 `from` 之后的所有 `type='in'` 流水，按 `occurred_at` 升序

#### Scenario: 时间区间闭合

- **WHEN** 调用方提供 `from` 与 `to`
- **THEN** 系统 MUST 仅返回 `occurred_at ∈ [from, to]` 的记录

### Requirement: 全局流水查询

The system MUST 支持不带 `accessory_id` 的全局流水查询，参数与按配件查询一致（时间范围、类型、分页）。

#### Scenario: 全局最新流水

- **WHEN** 调用方不带任何筛选条件查询全局流水
- **THEN** 系统 MUST 返回按 `occurred_at` 倒序的全量流水

### Requirement: 单条流水详情

The system MUST 支持按流水 `id` 查询单条详情，返回完整字段（含 `balance_after`）。

#### Scenario: 详情查询

- **WHEN** 调用方提供已存在的流水 `id`
- **THEN** 系统 MUST 返回该流水的完整字段

#### Scenario: 不存在

- **WHEN** 调用方提供不存在的流水 `id`
- **THEN** 系统 MUST 返回 `404 Not Found`

### Requirement: 流水不可变

系统 MUST 不提供修改或删除流水记录的接口；流水是审计来源。

#### Scenario: 无 update/delete 端点

- **WHEN** 调用方尝试调用流水修改或删除端点
- **THEN** 系统 MUST 返回 `405 Method Not Allowed`