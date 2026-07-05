-- 0001_init.sql — initial schema for warehouse
-- Creates accessories catalog and inventory_flow ledger.

CREATE TABLE IF NOT EXISTS accessories (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    sku                 TEXT    NOT NULL UNIQUE,
    name                TEXT    NOT NULL,
    current_stock       INTEGER NOT NULL DEFAULT 0,
    low_stock_threshold INTEGER NOT NULL DEFAULT 0,
    notes               TEXT    NOT NULL DEFAULT '',
    created_at          TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at          TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_accessories_sku ON accessories(sku);

CREATE TABLE IF NOT EXISTS inventory_flow (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    accessory_id   INTEGER NOT NULL REFERENCES accessories(id) ON DELETE RESTRICT,
    type           TEXT    NOT NULL CHECK (type IN ('in', 'out')),
    quantity       INTEGER NOT NULL CHECK (quantity > 0),
    unit_cost      REAL    NOT NULL DEFAULT 0,
    unit_price     REAL    NOT NULL DEFAULT 0,
    balance_after  INTEGER NOT NULL,
    client_ref     TEXT,
    remark         TEXT    NOT NULL DEFAULT '',
    occurred_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_flow_accessory_occurred
    ON inventory_flow(accessory_id, occurred_at);

CREATE UNIQUE INDEX IF NOT EXISTS uq_flow_client_ref
    ON inventory_flow(client_ref)
    WHERE client_ref IS NOT NULL;