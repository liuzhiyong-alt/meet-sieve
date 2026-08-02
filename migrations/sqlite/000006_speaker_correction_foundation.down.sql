-- 仅供开发测试回退；正式恢复必须使用升级前备份。
PRAGMA foreign_keys = OFF;

DROP TABLE speaker_track_evidence;
DROP TABLE correction_items;
DROP TABLE utterances;
DROP TABLE corrections;
DROP TABLE speaker_tracks;
DROP TABLE speaker_clusters;

ALTER TABLE utterances_step4_legacy RENAME TO utterances;
ALTER TABLE corrections_step4_legacy RENAME TO corrections;
ALTER TABLE speaker_clusters_step4_legacy RENAME TO speaker_clusters;

CREATE TABLE meeting_events_step4 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL,
    seq INTEGER NOT NULL CHECK(seq >= 1),
    kind TEXT NOT NULL CHECK(kind IN ('utterance.final', 'message.created', 'resource.created', 'utterance.corrected', 'speaker.corrected', 'resource.corrected', 'ai.question', 'ai.answer', 'ai.cancelled', 'asr.gap', 'asr.compensated')),
    occurred_at INTEGER NOT NULL CHECK(occurred_at >= 0),
    source TEXT NOT NULL CHECK(source IN ('host', 'guest', 'asr', 'agent', 'system')),
    entity_type TEXT NULL CHECK(entity_type IS NULL OR entity_type IN ('utterance', 'message', 'resource', 'correction', 'asr_gap', 'agent_turn', 'minute_version')),
    entity_id TEXT NULL, payload_json TEXT NULL CHECK(payload_json IS NULL OR json_valid(payload_json)),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, seq),
    CHECK((entity_type IS NULL AND entity_id IS NULL) OR (entity_type IS NOT NULL AND entity_id IS NOT NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
INSERT INTO meeting_events_step4 SELECT * FROM meeting_events WHERE kind <> 'speaker.attributed';
DROP TABLE meeting_events;
ALTER TABLE meeting_events_step4 RENAME TO meeting_events;

CREATE TABLE resources_step4 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, event_id TEXT NOT NULL UNIQUE,
    guest_session_id TEXT NULL, kind TEXT NOT NULL CHECK(kind IN ('link', 'attachment')),
    original_name TEXT NULL, safe_name TEXT NULL,
    relative_path TEXT NULL CHECK(relative_path IS NULL OR (relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0)),
    source_url TEXT NULL, media_type TEXT NULL, size_bytes INTEGER NULL CHECK(size_bytes IS NULL OR size_bytes >= 0),
    sha256 TEXT NULL CHECK(sha256 IS NULL OR (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*')),
    state TEXT NOT NULL CHECK(state IN ('ready', 'uploading', 'processing', 'completed', 'cancelled', 'failed')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((kind = 'link' AND source_url IS NOT NULL AND relative_path IS NULL) OR (kind = 'attachment' AND relative_path IS NOT NULL AND source_url IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(guest_session_id) REFERENCES guest_sessions(id) ON DELETE RESTRICT
);
INSERT INTO resources_step4 (
    id, meeting_id, event_id, guest_session_id, kind, original_name, safe_name, relative_path,
    source_url, media_type, size_bytes, sha256, state, created_at, updated_at
)
SELECT id, meeting_id, event_id, guest_session_id, kind, original_name, safe_name, relative_path,
       source_url, media_type, size_bytes, sha256, state, created_at, updated_at
FROM resources;
DROP TABLE resources;
ALTER TABLE resources_step4 RENAME TO resources;

CREATE TABLE voice_samples_step4 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), member_id TEXT NOT NULL,
    relative_path TEXT NOT NULL UNIQUE CHECK(relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0),
    duration_ms INTEGER NOT NULL CHECK(duration_ms > 0), sample_rate INTEGER NOT NULL CHECK(sample_rate = 16000),
    channels INTEGER NOT NULL CHECK(channels = 1), bit_depth INTEGER NOT NULL CHECK(bit_depth = 16),
    size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
    sha256 TEXT NOT NULL CHECK(length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    source_kind TEXT NOT NULL CHECK(source_kind IN ('recorded', 'imported')),
    source_name TEXT NULL CHECK(source_name IS NULL OR (trim(source_name) <> '' AND instr(source_name, '/') = 0 AND instr(source_name, '\') = 0)),
    environment_kind TEXT NOT NULL CHECK(environment_kind IN ('quiet', 'meeting_room', 'other')),
    processing_state TEXT NOT NULL CHECK(processing_state IN ('processing', 'ready', 'failed')),
    quality_state TEXT NOT NULL CHECK(quality_state IN ('pending', 'accepted', 'rejected')),
    quality_code TEXT NULL, quality_metrics_json TEXT NULL CHECK(quality_metrics_json IS NULL OR json_valid(quality_metrics_json)),
    last_error_code TEXT NULL, created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK(processing_state <> 'ready' OR quality_state IN ('accepted', 'rejected')),
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE CASCADE
);
INSERT INTO voice_samples_step4 (
    id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256,
    source_kind, source_name, environment_kind, processing_state, quality_state, quality_code,
    quality_metrics_json, last_error_code, created_at, updated_at
)
SELECT id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256,
       CASE WHEN source_kind = 'meeting_clip' THEN 'imported' ELSE source_kind END,
       source_name, environment_kind, processing_state, quality_state, quality_code,
       quality_metrics_json, last_error_code, created_at, updated_at
FROM voice_samples;

CREATE TABLE voice_embeddings_step4 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), voice_sample_id TEXT NOT NULL,
    model_id TEXT NOT NULL CHECK(trim(model_id) <> ''), model_version TEXT NOT NULL CHECK(trim(model_version) <> ''),
    model_sha256 TEXT NOT NULL CHECK(length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*'),
    dimension INTEGER NOT NULL CHECK(dimension > 0), embedding BLOB NOT NULL CHECK(length(embedding) = dimension * 4),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(voice_sample_id, model_id, model_version, model_sha256, dimension),
    FOREIGN KEY(voice_sample_id) REFERENCES voice_samples_step4(id) ON DELETE CASCADE
);
INSERT INTO voice_embeddings_step4 SELECT * FROM voice_embeddings;
DROP TABLE voice_embeddings;
DROP TABLE voice_samples;
ALTER TABLE voice_samples_step4 RENAME TO voice_samples;
ALTER TABLE voice_embeddings_step4 RENAME TO voice_embeddings;

CREATE INDEX idx_utterances_meeting_start_sample ON utterances(meeting_id, start_sample);
CREATE INDEX idx_utterances_asr_session_id ON utterances(asr_session_id);
CREATE INDEX idx_utterances_current_member_id ON utterances(current_member_id);
CREATE INDEX idx_corrections_target_created_at ON corrections(target_kind, target_id, created_at);
CREATE INDEX idx_corrections_meeting_id ON corrections(meeting_id);
CREATE INDEX idx_speaker_clusters_meeting_id ON speaker_clusters(meeting_id);
CREATE INDEX idx_speaker_clusters_asr_session_id ON speaker_clusters(asr_session_id);
CREATE INDEX idx_speaker_clusters_assigned_member_id ON speaker_clusters(assigned_member_id);
CREATE INDEX idx_meeting_events_meeting_occurred_at ON meeting_events(meeting_id, occurred_at);
CREATE INDEX idx_meeting_events_entity ON meeting_events(entity_type, entity_id);
CREATE INDEX idx_resources_meeting_state ON resources(meeting_id, state);
CREATE INDEX idx_resources_guest_session_id ON resources(guest_session_id);
CREATE INDEX idx_voice_samples_member_quality ON voice_samples(member_id, quality_state);
CREATE INDEX idx_voice_samples_processing_updated_at ON voice_samples(processing_state, updated_at);
CREATE INDEX idx_voice_embeddings_voice_sample_id ON voice_embeddings(voice_sample_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
