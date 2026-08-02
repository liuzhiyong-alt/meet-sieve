-- Step 5：重建说话人投影与校对事实。动态 UUID v5 和成员快照映射由升级 finalizer 完成。
PRAGMA foreign_keys = OFF;

ALTER TABLE speaker_clusters RENAME TO speaker_clusters_step4_legacy;
ALTER TABLE utterances RENAME TO utterances_step4_legacy;
ALTER TABLE corrections RENAME TO corrections_step4_legacy;

-- SQLite 重命名表时会保留全局索引名；staging 表不参与运行期查询，先释放旧名称。
DROP INDEX idx_speaker_clusters_meeting_id;
DROP INDEX idx_speaker_clusters_asr_session_id;
DROP INDEX idx_speaker_clusters_assigned_member_id;
DROP INDEX idx_utterances_meeting_start_sample;
DROP INDEX idx_utterances_asr_session_id;
DROP INDEX idx_utterances_current_member_id;
DROP INDEX idx_corrections_target_created_at;
DROP INDEX idx_corrections_meeting_id;

-- 自动 attribution 是独立统一事件，只触发 Timeline/Markdown 刷新，不伪装成人工 correction。
CREATE TABLE meeting_events_step5 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    seq INTEGER NOT NULL CHECK(seq >= 1),
    kind TEXT NOT NULL CHECK(kind IN ('utterance.final', 'speaker.attributed', 'message.created', 'resource.created', 'utterance.corrected', 'speaker.corrected', 'resource.corrected', 'ai.question', 'ai.answer', 'ai.cancelled', 'asr.gap', 'asr.compensated')),
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
INSERT INTO meeting_events_step5 SELECT * FROM meeting_events;
DROP TABLE meeting_events;
ALTER TABLE meeting_events_step5 RENAME TO meeting_events;
CREATE INDEX idx_meeting_events_meeting_occurred_at ON meeting_events(meeting_id, occurred_at);
CREATE INDEX idx_meeting_events_entity ON meeting_events(entity_type, entity_id);

CREATE TABLE speaker_clusters (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    display_no INTEGER NOT NULL CHECK(display_no >= 1),
    assigned_participant_id TEXT NULL,
    assignment_source TEXT NOT NULL CHECK(assignment_source IN ('unassigned', 'manual')),
    centroid BLOB NULL,
    model_id TEXT NULL CHECK(model_id IS NULL OR trim(model_id) <> ''),
    model_version TEXT NULL CHECK(model_version IS NULL OR trim(model_version) <> ''),
    model_sha256 TEXT NULL CHECK(model_sha256 IS NULL OR (length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*')),
    dimension INTEGER NULL CHECK(dimension IS NULL OR dimension > 0),
    profile_id TEXT NULL CHECK(profile_id IS NULL OR trim(profile_id) <> ''),
    track_count INTEGER NOT NULL DEFAULT 0 CHECK(track_count >= 0),
    confidence REAL NULL CHECK(confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision >= 1),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, display_no),
    CHECK((centroid IS NULL AND model_id IS NULL AND model_version IS NULL AND model_sha256 IS NULL AND dimension IS NULL AND profile_id IS NULL) OR
          (centroid IS NOT NULL AND model_id IS NOT NULL AND model_version IS NOT NULL AND model_sha256 IS NOT NULL AND dimension IS NOT NULL AND profile_id IS NOT NULL AND length(centroid) = dimension * 4)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(assigned_participant_id) REFERENCES meeting_participants(id) ON DELETE RESTRICT
);

CREATE TABLE speaker_tracks (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    asr_session_id TEXT NOT NULL,
    asr_speaker_label TEXT NOT NULL CHECK(trim(asr_speaker_label) <> ''),
    state TEXT NOT NULL CHECK(state IN ('collecting', 'pending', 'matched', 'clustered', 'ambiguous', 'insufficient', 'unavailable', 'failed', 'rebuild_required')),
    automatic_participant_id TEXT NULL,
    speaker_cluster_id TEXT NULL,
    top_score REAL NULL CHECK(top_score IS NULL OR (top_score >= -1 AND top_score <= 1)),
    runner_up_score REAL NULL CHECK(runner_up_score IS NULL OR (runner_up_score >= -1 AND runner_up_score <= 1)),
    evidence_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(evidence_duration_ms >= 0),
    model_id TEXT NULL CHECK(model_id IS NULL OR trim(model_id) <> ''),
    model_version TEXT NULL CHECK(model_version IS NULL OR trim(model_version) <> ''),
    model_sha256 TEXT NULL CHECK(model_sha256 IS NULL OR (length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*')),
    dimension INTEGER NULL CHECK(dimension IS NULL OR dimension > 0),
    embedding BLOB NULL,
    profile_id TEXT NULL CHECK(profile_id IS NULL OR trim(profile_id) <> ''),
    last_error_code TEXT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision >= 1),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(asr_session_id, asr_speaker_label),
    CHECK((embedding IS NULL AND model_id IS NULL AND model_version IS NULL AND model_sha256 IS NULL AND dimension IS NULL AND profile_id IS NULL) OR
          (embedding IS NOT NULL AND model_id IS NOT NULL AND model_version IS NOT NULL AND model_sha256 IS NOT NULL AND dimension IS NOT NULL AND profile_id IS NOT NULL AND length(embedding) = dimension * 4)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(automatic_participant_id) REFERENCES meeting_participants(id) ON DELETE RESTRICT,
    FOREIGN KEY(speaker_cluster_id) REFERENCES speaker_clusters(id) ON DELETE RESTRICT
);

CREATE TABLE utterances (
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
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(current_participant_id) REFERENCES meeting_participants(id) ON DELETE RESTRICT,
    FOREIGN KEY(speaker_track_id) REFERENCES speaker_tracks(id) ON DELETE RESTRICT,
    FOREIGN KEY(speaker_cluster_id) REFERENCES speaker_clusters(id) ON DELETE RESTRICT
);

CREATE TABLE speaker_track_evidence (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    speaker_track_id TEXT NOT NULL,
    utterance_id TEXT NOT NULL UNIQUE,
    evidence_order INTEGER NOT NULL CHECK(evidence_order >= 1),
    overlap_risk INTEGER NOT NULL CHECK(overlap_risk IN (0, 1)),
    included INTEGER NOT NULL CHECK(included IN (0, 1)),
    excluded_reason TEXT NULL CHECK(excluded_reason IS NULL OR trim(excluded_reason) <> ''),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(speaker_track_id, evidence_order),
    FOREIGN KEY(speaker_track_id) REFERENCES speaker_tracks(id) ON DELETE CASCADE,
    FOREIGN KEY(utterance_id) REFERENCES utterances(id) ON DELETE CASCADE
);

CREATE TABLE corrections (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    request_id TEXT NOT NULL UNIQUE CHECK(length(request_id) = 36),
    target_kind TEXT NOT NULL CHECK(target_kind IN ('utterance', 'speaker_cluster', 'message', 'resource')),
    target_id TEXT NOT NULL,
    correction_kind TEXT NOT NULL CHECK(correction_kind IN ('text', 'member_assignment', 'author_assignment', 'description')),
    before_json TEXT NOT NULL CHECK(json_valid(before_json)),
    after_json TEXT NOT NULL CHECK(json_valid(after_json)),
    operator_kind TEXT NOT NULL CHECK(operator_kind IN ('host', 'guest', 'system')),
    operator_id TEXT NULL,
    reason TEXT NULL,
    target_revision INTEGER NOT NULL CHECK(target_revision >= 1),
    result_revision INTEGER NOT NULL CHECK(result_revision = target_revision + 1),
    batch_scope TEXT NOT NULL CHECK(batch_scope IN ('single', 'speaker_cluster')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((operator_kind = 'system' AND operator_id IS NULL) OR (operator_kind IN ('host', 'guest') AND operator_id IS NOT NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT
);

CREATE TABLE correction_items (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    correction_id TEXT NOT NULL,
    target_kind TEXT NOT NULL CHECK(target_kind IN ('utterance', 'speaker_cluster', 'resource')),
    target_id TEXT NOT NULL,
    before_json TEXT NOT NULL CHECK(json_valid(before_json)),
    after_json TEXT NOT NULL CHECK(json_valid(after_json)),
    item_order INTEGER NOT NULL CHECK(item_order >= 1),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(correction_id, target_kind, target_id),
    UNIQUE(correction_id, item_order),
    FOREIGN KEY(correction_id) REFERENCES corrections(id) ON DELETE CASCADE
);

CREATE TABLE resources_step5 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, event_id TEXT NOT NULL UNIQUE,
    guest_session_id TEXT NULL, kind TEXT NOT NULL CHECK(kind IN ('link', 'attachment')),
    original_name TEXT NULL, safe_name TEXT NULL,
    relative_path TEXT NULL CHECK(relative_path IS NULL OR (relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0)),
    source_url TEXT NULL, media_type TEXT NULL, size_bytes INTEGER NULL CHECK(size_bytes IS NULL OR size_bytes >= 0),
    sha256 TEXT NULL CHECK(sha256 IS NULL OR (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*')),
    original_description TEXT NULL, current_description TEXT NULL,
    description_revision INTEGER NOT NULL DEFAULT 1 CHECK(description_revision >= 1),
    state TEXT NOT NULL CHECK(state IN ('ready', 'uploading', 'processing', 'completed', 'cancelled', 'failed')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((kind = 'link' AND source_url IS NOT NULL AND relative_path IS NULL) OR (kind = 'attachment' AND relative_path IS NOT NULL AND source_url IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(guest_session_id) REFERENCES guest_sessions(id) ON DELETE RESTRICT
);
INSERT INTO resources_step5 (
    id, meeting_id, event_id, guest_session_id, kind, original_name, safe_name, relative_path, source_url,
    media_type, size_bytes, sha256, original_description, current_description, description_revision, state, created_at, updated_at
)
SELECT id, meeting_id, event_id, guest_session_id, kind, original_name, safe_name, relative_path, source_url,
       media_type, size_bytes, sha256, NULL, NULL, 1, state, created_at, updated_at
FROM resources;
DROP TABLE resources;
ALTER TABLE resources_step5 RENAME TO resources;

CREATE TABLE voice_samples_step5 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), member_id TEXT NOT NULL,
    relative_path TEXT NOT NULL UNIQUE CHECK(relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0),
    duration_ms INTEGER NOT NULL CHECK(duration_ms > 0), sample_rate INTEGER NOT NULL CHECK(sample_rate = 16000),
    channels INTEGER NOT NULL CHECK(channels = 1), bit_depth INTEGER NOT NULL CHECK(bit_depth = 16),
    size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
    sha256 TEXT NOT NULL CHECK(length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    source_kind TEXT NOT NULL CHECK(source_kind IN ('recorded', 'imported', 'meeting_clip')),
    source_name TEXT NULL CHECK(source_name IS NULL OR (trim(source_name) <> '' AND instr(source_name, '/') = 0 AND instr(source_name, '\') = 0)),
    request_id TEXT NULL UNIQUE CHECK(request_id IS NULL OR length(request_id) = 36),
    source_meeting_id TEXT NULL, source_utterance_id TEXT NULL,
    environment_kind TEXT NOT NULL CHECK(environment_kind IN ('quiet', 'meeting_room', 'other')),
    processing_state TEXT NOT NULL CHECK(processing_state IN ('processing', 'ready', 'failed')),
    quality_state TEXT NOT NULL CHECK(quality_state IN ('pending', 'accepted', 'rejected')),
    quality_code TEXT NULL, quality_metrics_json TEXT NULL CHECK(quality_metrics_json IS NULL OR json_valid(quality_metrics_json)),
    last_error_code TEXT NULL, created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK(processing_state <> 'ready' OR quality_state IN ('accepted', 'rejected')),
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE CASCADE,
    FOREIGN KEY(source_meeting_id) REFERENCES meetings(id) ON DELETE SET NULL,
    FOREIGN KEY(source_utterance_id) REFERENCES utterances(id) ON DELETE SET NULL
);
INSERT INTO voice_samples_step5 (
    id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256,
    source_kind, source_name, request_id, source_meeting_id, source_utterance_id, environment_kind, processing_state,
    quality_state, quality_code, quality_metrics_json, last_error_code, created_at, updated_at
)
SELECT id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256,
       source_kind, source_name, NULL, NULL, NULL, environment_kind, processing_state, quality_state, quality_code,
       quality_metrics_json, last_error_code, created_at, updated_at
FROM voice_samples;

CREATE TABLE voice_embeddings_step5 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), voice_sample_id TEXT NOT NULL,
    model_id TEXT NOT NULL CHECK(trim(model_id) <> ''), model_version TEXT NOT NULL CHECK(trim(model_version) <> ''),
    model_sha256 TEXT NOT NULL CHECK(length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*'),
    dimension INTEGER NOT NULL CHECK(dimension > 0), embedding BLOB NOT NULL CHECK(length(embedding) = dimension * 4),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(voice_sample_id, model_id, model_version, model_sha256, dimension),
    FOREIGN KEY(voice_sample_id) REFERENCES voice_samples_step5(id) ON DELETE CASCADE
);
INSERT INTO voice_embeddings_step5 SELECT * FROM voice_embeddings;
DROP TABLE voice_embeddings;
DROP TABLE voice_samples;
ALTER TABLE voice_samples_step5 RENAME TO voice_samples;
ALTER TABLE voice_embeddings_step5 RENAME TO voice_embeddings;

CREATE INDEX idx_speaker_clusters_meeting_id ON speaker_clusters(meeting_id);
CREATE INDEX idx_speaker_clusters_assigned_participant_id ON speaker_clusters(assigned_participant_id);
CREATE INDEX idx_speaker_tracks_meeting_id ON speaker_tracks(meeting_id);
CREATE INDEX idx_speaker_tracks_asr_session_id ON speaker_tracks(asr_session_id);
CREATE INDEX idx_speaker_tracks_automatic_participant_id ON speaker_tracks(automatic_participant_id);
CREATE INDEX idx_speaker_tracks_speaker_cluster_id ON speaker_tracks(speaker_cluster_id);
CREATE INDEX idx_speaker_track_evidence_track_id ON speaker_track_evidence(speaker_track_id);
CREATE INDEX idx_speaker_track_evidence_utterance_id ON speaker_track_evidence(utterance_id);
CREATE INDEX idx_utterances_meeting_start_sample ON utterances(meeting_id, start_sample);
CREATE INDEX idx_utterances_event_id ON utterances(event_id);
CREATE INDEX idx_utterances_asr_session_id ON utterances(asr_session_id);
CREATE INDEX idx_utterances_current_participant_id ON utterances(current_participant_id);
CREATE INDEX idx_utterances_speaker_track_id ON utterances(speaker_track_id);
CREATE INDEX idx_utterances_speaker_cluster_id ON utterances(speaker_cluster_id);
CREATE INDEX idx_corrections_target_created_at ON corrections(target_kind, target_id, created_at);
CREATE INDEX idx_corrections_meeting_id ON corrections(meeting_id);
CREATE INDEX idx_corrections_event_id ON corrections(event_id);
CREATE INDEX idx_correction_items_correction_id ON correction_items(correction_id);
CREATE INDEX idx_resources_meeting_state ON resources(meeting_id, state);
CREATE INDEX idx_resources_event_id ON resources(event_id);
CREATE INDEX idx_resources_guest_session_id ON resources(guest_session_id);
CREATE INDEX idx_voice_samples_member_quality ON voice_samples(member_id, quality_state);
CREATE INDEX idx_voice_samples_processing_updated_at ON voice_samples(processing_state, updated_at);
CREATE INDEX idx_voice_samples_source_meeting_id ON voice_samples(source_meeting_id);
CREATE INDEX idx_voice_samples_source_utterance_id ON voice_samples(source_utterance_id);
CREATE INDEX idx_voice_embeddings_voice_sample_id ON voice_embeddings(voice_sample_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
