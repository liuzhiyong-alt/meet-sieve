-- Step 8 回退仅用于开发临时库；attempt 事实与极速文件 session 可能丢失。
PRAGMA foreign_keys = OFF;

DROP TABLE IF EXISTS gap_transcription_attempt_items;
DROP TABLE IF EXISTS gap_transcription_attempts;
DROP INDEX IF EXISTS idx_minute_versions_meeting_created;

-- v8 不认识极速文件 session；先清除或解绑其 Step 8 派生事实，再恢复旧 CHECK。
DELETE FROM speaker_track_evidence
WHERE speaker_track_id IN (
    SELECT id FROM speaker_tracks WHERE asr_session_id IN (
        SELECT id FROM asr_sessions WHERE transport_mode = 'auc_flash_v3'
    )
);
DELETE FROM speaker_tracks WHERE asr_session_id IN (
    SELECT id FROM asr_sessions WHERE transport_mode = 'auc_flash_v3'
);
DELETE FROM utterances WHERE asr_session_id IN (
    SELECT id FROM asr_sessions WHERE transport_mode = 'auc_flash_v3'
);
UPDATE asr_gaps SET asr_session_id = NULL
WHERE asr_session_id IN (
    SELECT id FROM asr_sessions WHERE transport_mode = 'auc_flash_v3'
);
DELETE FROM asr_sessions WHERE transport_mode = 'auc_flash_v3';

CREATE TABLE asr_sessions_step7 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK(provider = 'volcano'),
    provider_session_id TEXT NULL,
    state TEXT NOT NULL CHECK(state IN ('connecting', 'streaming', 'disconnected', 'stopped', 'failed')),
    started_at INTEGER NOT NULL CHECK(started_at >= 0),
    ended_at INTEGER NULL CHECK(ended_at IS NULL OR ended_at >= started_at),
    reconnect_count INTEGER NOT NULL DEFAULT 0 CHECK(reconnect_count >= 0),
    transport_mode TEXT NOT NULL CHECK(transport_mode = 'seed_v1'),
    input_start_sample INTEGER NOT NULL CHECK(input_start_sample >= 0),
    last_sent_sample INTEGER NOT NULL CHECK(last_sent_sample >= input_start_sample),
    last_final_sample INTEGER NOT NULL CHECK(last_final_sample >= input_start_sample AND last_final_sample <= last_sent_sample),
    last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(provider, provider_session_id),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
INSERT INTO asr_sessions_step7 (
    id, meeting_id, provider, provider_session_id, state, started_at, ended_at,
    reconnect_count, transport_mode, input_start_sample, last_sent_sample,
    last_final_sample, last_error_code, created_at, updated_at
)
SELECT id, meeting_id, provider, provider_session_id, state, started_at, ended_at,
       reconnect_count, transport_mode, input_start_sample, last_sent_sample,
       last_final_sample, last_error_code, created_at, updated_at
FROM asr_sessions;
DROP TABLE asr_sessions;
ALTER TABLE asr_sessions_step7 RENAME TO asr_sessions;
CREATE INDEX idx_asr_sessions_meeting_started_at ON asr_sessions(meeting_id, started_at);

CREATE TABLE minute_versions_step7 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL,
    agent_turn_id TEXT NULL, parent_version_id TEXT NULL,
    version_no INTEGER NOT NULL CHECK(version_no >= 1),
    source TEXT NOT NULL CHECK(source IN ('ai', 'human', 'restored')),
    content_markdown TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('draft', 'confirmed')),
    is_current INTEGER NOT NULL DEFAULT 1 CHECK(is_current IN (0, 1)),
    confirmed_at INTEGER NULL CHECK(confirmed_at IS NULL OR confirmed_at >= 0),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, version_no),
    CHECK((state = 'confirmed' AND confirmed_at IS NOT NULL) OR (state = 'draft' AND confirmed_at IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    FOREIGN KEY(parent_version_id) REFERENCES minute_versions_step7(id) ON DELETE RESTRICT
);
INSERT INTO minute_versions_step7 (
    id, meeting_id, agent_turn_id, parent_version_id, version_no, source,
    content_markdown, state, is_current, confirmed_at, created_at, updated_at
)
SELECT id, meeting_id, agent_turn_id, parent_version_id, version_no, source,
       content_markdown, state, is_current, confirmed_at, created_at, updated_at
FROM minute_versions;
DROP TABLE minute_versions;
ALTER TABLE minute_versions_step7 RENAME TO minute_versions;
CREATE UNIQUE INDEX idx_minute_versions_current_meeting ON minute_versions(meeting_id) WHERE is_current = 1;
CREATE INDEX idx_minute_versions_agent_turn_id ON minute_versions(agent_turn_id);
CREATE INDEX idx_minute_versions_parent_version_id ON minute_versions(parent_version_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
