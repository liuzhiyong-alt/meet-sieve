-- Step 8：扩展文件补转写 session，新增可审计 attempt，并收紧不可变纪要来源约束。
-- SQLite 无法直接修改 CHECK，以下重建显式列出全部字段并保留 Step 7 数据。
PRAGMA foreign_keys = OFF;

CREATE TABLE asr_sessions_step8 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK(provider = 'volcano'),
    provider_session_id TEXT NULL,
    state TEXT NOT NULL CHECK(state IN ('connecting', 'streaming', 'disconnected', 'stopped', 'failed')),
    started_at INTEGER NOT NULL CHECK(started_at >= 0),
    ended_at INTEGER NULL CHECK(ended_at IS NULL OR ended_at >= started_at),
    reconnect_count INTEGER NOT NULL DEFAULT 0 CHECK(reconnect_count >= 0),
    transport_mode TEXT NOT NULL CHECK(transport_mode IN ('seed_v1', 'auc_flash_v3')),
    input_start_sample INTEGER NOT NULL CHECK(input_start_sample >= 0),
    last_sent_sample INTEGER NOT NULL CHECK(last_sent_sample >= input_start_sample),
    last_final_sample INTEGER NOT NULL CHECK(last_final_sample >= input_start_sample AND last_final_sample <= last_sent_sample),
    last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(provider, provider_session_id),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
INSERT INTO asr_sessions_step8 (
    id, meeting_id, provider, provider_session_id, state, started_at, ended_at,
    reconnect_count, transport_mode, input_start_sample, last_sent_sample,
    last_final_sample, last_error_code, created_at, updated_at
)
SELECT id, meeting_id, provider, provider_session_id, state, started_at, ended_at,
       reconnect_count, transport_mode, input_start_sample, last_sent_sample,
       last_final_sample, last_error_code, created_at, updated_at
FROM asr_sessions;
DROP TABLE asr_sessions;
ALTER TABLE asr_sessions_step8 RENAME TO asr_sessions;
CREATE INDEX idx_asr_sessions_meeting_started_at ON asr_sessions(meeting_id, started_at);

CREATE TABLE minute_versions_step8 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    agent_turn_id TEXT NULL,
    parent_version_id TEXT NULL,
    version_no INTEGER NOT NULL CHECK(version_no >= 1),
    source TEXT NOT NULL CHECK(source IN ('ai', 'human', 'restored')),
    content_markdown TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('draft', 'confirmed')),
    is_current INTEGER NOT NULL DEFAULT 1 CHECK(is_current IN (0, 1)),
    confirmed_at INTEGER NULL CHECK(confirmed_at IS NULL OR confirmed_at >= 0),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, version_no),
    CHECK((state = 'confirmed' AND confirmed_at IS NOT NULL) OR (state = 'draft' AND confirmed_at IS NULL)),
    CHECK(source <> 'ai' OR agent_turn_id IS NOT NULL),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    FOREIGN KEY(parent_version_id) REFERENCES minute_versions_step8(id) ON DELETE RESTRICT
);
INSERT INTO minute_versions_step8 (
    id, meeting_id, agent_turn_id, parent_version_id, version_no, source,
    content_markdown, state, is_current, confirmed_at, created_at, updated_at
)
SELECT id, meeting_id, agent_turn_id, parent_version_id, version_no, source,
       content_markdown, state, is_current, confirmed_at, created_at, updated_at
FROM minute_versions;
DROP TABLE minute_versions;
ALTER TABLE minute_versions_step8 RENAME TO minute_versions;
CREATE UNIQUE INDEX idx_minute_versions_current_meeting ON minute_versions(meeting_id) WHERE is_current = 1;
CREATE INDEX idx_minute_versions_agent_turn_id ON minute_versions(agent_turn_id);
CREATE INDEX idx_minute_versions_parent_version_id ON minute_versions(parent_version_id);
CREATE INDEX idx_minute_versions_meeting_created ON minute_versions(meeting_id, created_at DESC, version_no DESC);

CREATE TABLE gap_transcription_attempts (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    audio_asset_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK(provider = 'volcano'),
    provider_request_id TEXT NOT NULL UNIQUE CHECK(length(provider_request_id) = 36),
    core_start_sample INTEGER NOT NULL CHECK(core_start_sample >= 0),
    core_end_sample INTEGER NOT NULL CHECK(core_end_sample > core_start_sample),
    audio_start_sample INTEGER NOT NULL CHECK(audio_start_sample >= 0 AND audio_start_sample <= core_start_sample),
    audio_end_sample INTEGER NOT NULL CHECK(audio_end_sample >= core_end_sample),
    state TEXT NOT NULL CHECK(state IN ('pending', 'running', 'completed', 'failed', 'conflict', 'cancelled')),
    attempt_no INTEGER NOT NULL CHECK(attempt_no >= 1),
    request_sha256 TEXT NOT NULL UNIQUE CHECK(length(request_sha256) = 64 AND request_sha256 NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NULL CHECK(response_json IS NULL OR json_valid(response_json)),
    provider_log_id_suffix TEXT NULL CHECK(provider_log_id_suffix IS NULL OR length(provider_log_id_suffix) <= 16),
    last_error_code TEXT NULL,
    started_at INTEGER NULL CHECK(started_at IS NULL OR started_at >= 0),
    ended_at INTEGER NULL CHECK(ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(audio_asset_id) REFERENCES audio_assets(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX idx_gap_attempts_active_meeting
    ON gap_transcription_attempts(meeting_id) WHERE state IN ('pending', 'running');
CREATE INDEX idx_gap_attempts_meeting_state_created
    ON gap_transcription_attempts(meeting_id, state, created_at);
CREATE INDEX idx_gap_attempts_audio_asset_id ON gap_transcription_attempts(audio_asset_id);

CREATE TABLE gap_transcription_attempt_items (
    attempt_id TEXT NOT NULL,
    gap_id TEXT NOT NULL,
    item_order INTEGER NOT NULL CHECK(item_order >= 0),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    PRIMARY KEY(attempt_id, gap_id),
    UNIQUE(attempt_id, item_order),
    FOREIGN KEY(attempt_id) REFERENCES gap_transcription_attempts(id) ON DELETE CASCADE,
    FOREIGN KEY(gap_id) REFERENCES asr_gaps(id) ON DELETE RESTRICT
);
CREATE INDEX idx_gap_attempt_items_gap_id ON gap_transcription_attempt_items(gap_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
