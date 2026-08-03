-- Step 7：扩展智能体失败事件、滚动快照和单会议活动约束。
PRAGMA foreign_keys = OFF;

CREATE TABLE meeting_events_step7 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    seq INTEGER NOT NULL CHECK(seq >= 1),
    kind TEXT NOT NULL CHECK(kind IN ('utterance.final', 'speaker.attributed', 'message.created', 'resource.created', 'utterance.corrected', 'speaker.corrected', 'resource.corrected', 'ai.question', 'ai.answer', 'ai.cancelled', 'ai.failed', 'asr.gap', 'asr.compensated')),
    occurred_at INTEGER NOT NULL CHECK(occurred_at >= 0),
    source TEXT NOT NULL CHECK(source IN ('host', 'guest', 'asr', 'agent', 'system')),
    entity_type TEXT NULL CHECK(entity_type IS NULL OR entity_type IN ('utterance', 'speaker_track', 'message', 'resource', 'correction', 'asr_gap', 'agent_turn', 'minute_version')),
    entity_id TEXT NULL,
    payload_json TEXT NULL CHECK(payload_json IS NULL OR json_valid(payload_json)),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, seq),
    CHECK((entity_type IS NULL AND entity_id IS NULL) OR (entity_type IS NOT NULL AND entity_id IS NOT NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
INSERT INTO meeting_events_step7 (
    id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id,
    payload_json, created_at, updated_at
)
SELECT id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id,
       payload_json, created_at, updated_at
FROM meeting_events;
DROP TABLE meeting_events;
ALTER TABLE meeting_events_step7 RENAME TO meeting_events;
CREATE INDEX idx_meeting_events_meeting_occurred_at ON meeting_events(meeting_id, occurred_at);
CREATE INDEX idx_meeting_events_entity ON meeting_events(entity_type, entity_id);

CREATE TABLE context_snapshots_step7 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    agent_session_id TEXT NOT NULL UNIQUE,
    agent_turn_id TEXT NOT NULL,
    through_seq INTEGER NOT NULL CHECK(through_seq >= 0),
    content_json TEXT NOT NULL CHECK(json_valid(content_json)),
    content_sha256 TEXT NOT NULL CHECK(length(content_sha256) = 64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_session_id) REFERENCES agent_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT
);
INSERT INTO context_snapshots_step7 (
    id, meeting_id, agent_session_id, agent_turn_id, through_seq,
    content_json, content_sha256, created_at, updated_at
)
SELECT id, meeting_id, agent_session_id, agent_turn_id, through_seq,
       content_json, content_sha256, created_at, updated_at
FROM context_snapshots;
DROP TABLE context_snapshots;
ALTER TABLE context_snapshots_step7 RENAME TO context_snapshots;
CREATE INDEX idx_context_snapshots_meeting_id ON context_snapshots(meeting_id);
CREATE INDEX idx_context_snapshots_agent_turn_id ON context_snapshots(agent_turn_id);

CREATE UNIQUE INDEX idx_agent_sessions_active_meeting
    ON agent_sessions(meeting_id) WHERE state IN ('starting', 'available');
CREATE UNIQUE INDEX idx_agent_turns_active_meeting
    ON agent_turns(meeting_id) WHERE state IN ('pending', 'running');
CREATE INDEX idx_agent_turns_meeting_state_created_at
    ON agent_turns(meeting_id, state, created_at);

-- 修复旧版本重建父表时被 SQLite 改写成临时表名的两个外键。
-- 表结构与 Step 6 保持一致，只纠正引用目标并完整保留数据。
CREATE TABLE asr_gaps_step7 (
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
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(audio_asset_id) REFERENCES audio_assets(id) ON DELETE RESTRICT
);
INSERT INTO asr_gaps_step7 SELECT * FROM asr_gaps;
DROP TABLE asr_gaps;
ALTER TABLE asr_gaps_step7 RENAME TO asr_gaps;
CREATE INDEX idx_asr_gaps_meeting_state ON asr_gaps(meeting_id, state);
CREATE INDEX idx_asr_gaps_asr_session_id ON asr_gaps(asr_session_id);
CREATE INDEX idx_asr_gaps_audio_asset_id ON asr_gaps(audio_asset_id);

CREATE TABLE voice_embeddings_step7 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), voice_sample_id TEXT NOT NULL,
    model_id TEXT NOT NULL CHECK(trim(model_id) <> ''), model_version TEXT NOT NULL CHECK(trim(model_version) <> ''),
    model_sha256 TEXT NOT NULL CHECK(length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*'),
    dimension INTEGER NOT NULL CHECK(dimension > 0), embedding BLOB NOT NULL CHECK(length(embedding) = dimension * 4),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(voice_sample_id, model_id, model_version, model_sha256, dimension),
    FOREIGN KEY(voice_sample_id) REFERENCES voice_samples(id) ON DELETE CASCADE
);
INSERT INTO voice_embeddings_step7 SELECT * FROM voice_embeddings;
DROP TABLE voice_embeddings;
ALTER TABLE voice_embeddings_step7 RENAME TO voice_embeddings;
CREATE INDEX idx_voice_embeddings_voice_sample_id ON voice_embeddings(voice_sample_id);

-- 同一会议删除时，event/asr_session 与 utterance 都会级联；子引用必须同步级联，
-- 否则 SQLite 的即时 RESTRICT 会在父级联顺序尚未完成时阻断会议删除。
CREATE TABLE utterances_step7 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    asr_session_id TEXT NOT NULL,
    provider_result_id TEXT NOT NULL,
    original_text TEXT NOT NULL CHECK(trim(original_text) <> ''),
    current_text TEXT NOT NULL CHECK(trim(current_text) <> ''),
    start_sample INTEGER NOT NULL CHECK(start_sample >= 0),
    end_sample INTEGER NOT NULL CHECK(end_sample > start_sample),
    asr_speaker_label TEXT NULL,
    current_participant_id TEXT NULL,
    speaker_track_id TEXT NULL,
    speaker_cluster_id TEXT NULL,
    speaker_assignment_source TEXT NOT NULL DEFAULT 'unassigned' CHECK(speaker_assignment_source IN ('unassigned', 'automatic_member', 'automatic_cluster', 'manual_single', 'manual_cluster')),
    speaker_confidence REAL NULL CHECK(speaker_confidence IS NULL OR (speaker_confidence >= -1 AND speaker_confidence <= 1)),
    text_revision INTEGER NOT NULL DEFAULT 1 CHECK(text_revision >= 1),
    speaker_revision INTEGER NOT NULL DEFAULT 1 CHECK(speaker_revision >= 1),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(asr_session_id, provider_result_id),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE CASCADE,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE CASCADE,
    FOREIGN KEY(current_participant_id) REFERENCES meeting_participants(id) ON DELETE RESTRICT,
    FOREIGN KEY(speaker_track_id) REFERENCES speaker_tracks(id) ON DELETE RESTRICT,
    FOREIGN KEY(speaker_cluster_id) REFERENCES speaker_clusters(id) ON DELETE RESTRICT
);
INSERT INTO utterances_step7 SELECT * FROM utterances;
DROP TABLE utterances;
ALTER TABLE utterances_step7 RENAME TO utterances;
CREATE INDEX idx_utterances_meeting_start_sample ON utterances(meeting_id, start_sample);
CREATE INDEX idx_utterances_event_id ON utterances(event_id);
CREATE INDEX idx_utterances_asr_session_id ON utterances(asr_session_id);
CREATE INDEX idx_utterances_current_participant_id ON utterances(current_participant_id);
CREATE INDEX idx_utterances_speaker_track_id ON utterances(speaker_track_id);
CREATE INDEX idx_utterances_speaker_cluster_id ON utterances(speaker_cluster_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
