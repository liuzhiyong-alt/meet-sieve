-- SQLite 3.35+ 支持删除列；该回退仅用于开发测试。
ALTER TABLE settings DROP COLUMN codex_probed_at;
ALTER TABLE settings DROP COLUMN codex_probe_message;
ALTER TABLE settings DROP COLUMN codex_protocol_state;
ALTER TABLE settings DROP COLUMN codex_account_state;
ALTER TABLE settings DROP COLUMN codex_version;
ALTER TABLE settings DROP COLUMN codex_availability_state;
