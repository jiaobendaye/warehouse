-- 0002_add_stall.sql — add stall (档口) field to accessories.
-- File-outbound auto-created accessories get the stall from the 汇总 sheet
-- column header; manually created and file-inbound accessories default to
-- "未分配" until the user assigns one.

ALTER TABLE accessories ADD COLUMN stall TEXT NOT NULL DEFAULT '未分配';
