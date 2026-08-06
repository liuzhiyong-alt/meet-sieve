-- 持久化脱敏的 Codex 可用性快照，避免设置页重建时退回“尚未检测”。
ALTER TABLE settings ADD COLUMN codex_availability_state TEXT NOT NULL DEFAULT 'unchecked'
    CHECK(codex_availability_state IN ('unchecked', 'available', 'unavailable'));
ALTER TABLE settings ADD COLUMN codex_version TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN codex_account_state TEXT NOT NULL DEFAULT 'unknown'
    CHECK(codex_account_state IN ('unknown', 'logged_in', 'logged_out'));
ALTER TABLE settings ADD COLUMN codex_protocol_state TEXT NOT NULL DEFAULT 'unchecked'
    CHECK(codex_protocol_state IN ('unchecked', 'compatible', 'incompatible'));
ALTER TABLE settings ADD COLUMN codex_probe_message TEXT NOT NULL DEFAULT '尚未检测';
ALTER TABLE settings ADD COLUMN codex_probed_at INTEGER NULL CHECK(codex_probed_at IS NULL OR codex_probed_at >= 0);
