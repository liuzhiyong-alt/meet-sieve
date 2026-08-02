-- Step 4：SQLite 不支持为既有 CHECK 约束增列，故重建 ASR session/gap 表。
-- migration 连接独立于运行期连接，临时关闭外键只用于表替换，结尾重新启用并执行检查。
PRAGMA foreign_keys = OFF;

CREATE TABLE asr_sessions_step4 (
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

INSERT INTO asr_sessions_step4 (
    id, meeting_id, provider, provider_session_id, state, started_at, ended_at, reconnect_count,
    transport_mode, input_start_sample, last_sent_sample, last_final_sample, last_error_code, created_at, updated_at
)
SELECT
    session.id, session.meeting_id, session.provider, session.provider_session_id, session.state,
    session.started_at, session.ended_at, session.reconnect_count, 'seed_v1',
    COALESCE((SELECT MIN(utterance.start_sample) FROM utterances AS utterance WHERE utterance.asr_session_id = session.id), 0),
    COALESCE((SELECT MAX(utterance.end_sample) FROM utterances AS utterance WHERE utterance.asr_session_id = session.id), 0),
    COALESCE((SELECT MAX(utterance.end_sample) FROM utterances AS utterance WHERE utterance.asr_session_id = session.id), 0),
    session.last_error_code, session.created_at, session.updated_at
FROM asr_sessions AS session;

CREATE TABLE asr_gaps_step4 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    asr_session_id TEXT NULL,
    audio_asset_id TEXT NULL,
    start_sample INTEGER NOT NULL CHECK(start_sample >= 0),
    end_sample INTEGER NOT NULL CHECK(end_sample > start_sample),
    reason TEXT NOT NULL CHECK(reason IN ('connect_failed', 'disconnected', 'backpressure', 'tail_timeout', 'recovery', 'record_only')),
    origin_key TEXT NOT NULL UNIQUE CHECK(length(origin_key) = 64 AND origin_key NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK(state IN ('pending', 'processing', 'completed', 'failed', 'conflict')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0),
    result_from_seq INTEGER NULL,
    result_to_seq INTEGER NULL,
    conflict_json TEXT NULL CHECK(conflict_json IS NULL OR json_valid(conflict_json)),
    last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((result_from_seq IS NULL AND result_to_seq IS NULL) OR (result_from_seq IS NOT NULL AND result_to_seq IS NOT NULL AND result_from_seq >= 1 AND result_to_seq >= result_from_seq)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions_step4(id) ON DELETE RESTRICT,
    FOREIGN KEY(audio_asset_id) REFERENCES audio_assets(id) ON DELETE RESTRICT
);

-- Step 4 之前不存在 origin key；UUID 派生值只服务历史记录唯一性，新增 gap 一律由 service 写入 SHA-256。
INSERT INTO asr_gaps_step4 (
    id, meeting_id, event_id, asr_session_id, audio_asset_id, start_sample, end_sample, reason, origin_key,
    state, attempt_count, result_from_seq, result_to_seq, conflict_json, last_error_code, created_at, updated_at
)
SELECT
    id, meeting_id, event_id, asr_session_id, audio_asset_id, start_sample, end_sample, 'recovery',
    lower('00000000000000000000000000000000' || replace(id, '-', '')),
    state, attempt_count, result_from_seq, result_to_seq, conflict_json, last_error_code, created_at, updated_at
FROM asr_gaps;

DROP TABLE asr_gaps;
DROP TABLE asr_sessions;
ALTER TABLE asr_sessions_step4 RENAME TO asr_sessions;
ALTER TABLE asr_gaps_step4 RENAME TO asr_gaps;

CREATE INDEX idx_asr_sessions_meeting_started_at ON asr_sessions(meeting_id, started_at);
CREATE INDEX idx_asr_gaps_meeting_state ON asr_gaps(meeting_id, state);
CREATE INDEX idx_asr_gaps_asr_session_id ON asr_gaps(asr_session_id);
CREATE INDEX idx_asr_gaps_audio_asset_id ON asr_gaps(audio_asset_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
