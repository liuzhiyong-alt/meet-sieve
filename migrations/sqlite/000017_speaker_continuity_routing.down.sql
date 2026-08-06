-- 产生第二 segment 后旧 schema 无法无损表达，明确拒绝回退。
CREATE TEMP TABLE speaker_v17_down_guard(value INTEGER CHECK(value = 1));
INSERT INTO speaker_v17_down_guard
SELECT CASE WHEN EXISTS(SELECT 1 FROM speaker_tracks WHERE provider_segment_no > 1) THEN 0 ELSE 1 END;
DROP TABLE speaker_v17_down_guard;

CREATE TABLE speaker_track_evidence_v16 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    speaker_track_id TEXT NOT NULL,
    utterance_id TEXT NOT NULL UNIQUE,
    evidence_order INTEGER NOT NULL CHECK(evidence_order >= 1),
    overlap_risk INTEGER NOT NULL DEFAULT 0 CHECK(overlap_risk IN (0, 1)),
    included INTEGER NOT NULL DEFAULT 0 CHECK(included IN (0, 1)),
    excluded_reason TEXT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(speaker_track_id, evidence_order),
    FOREIGN KEY(speaker_track_id) REFERENCES speaker_tracks(id) ON DELETE CASCADE,
    FOREIGN KEY(utterance_id) REFERENCES utterances(id) ON DELETE RESTRICT
);
INSERT INTO speaker_track_evidence_v16(id, speaker_track_id, utterance_id, evidence_order, overlap_risk, included, excluded_reason, created_at, updated_at)
SELECT id, speaker_track_id, utterance_id, evidence_order, overlap_risk, included, excluded_reason, created_at, updated_at
FROM speaker_track_evidence;
DROP TABLE speaker_track_evidence;
ALTER TABLE speaker_track_evidence_v16 RENAME TO speaker_track_evidence;
CREATE INDEX idx_speaker_track_evidence_track_id ON speaker_track_evidence(speaker_track_id);

CREATE TABLE speaker_tracks_v16 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, asr_session_id TEXT NOT NULL,
    source TEXT NOT NULL, asr_speaker_label TEXT NULL, source_utterance_id TEXT NULL,
    display_no INTEGER NULL, state TEXT NOT NULL, automatic_participant_id TEXT NULL, speaker_cluster_id TEXT NULL,
    top_score REAL NULL, runner_up_score REAL NULL, evidence_duration_ms INTEGER NOT NULL DEFAULT 0,
    model_id TEXT NULL, model_version TEXT NULL, model_sha256 TEXT NULL, dimension INTEGER NULL,
    embedding BLOB NULL, profile_id TEXT NULL, last_error_code TEXT NULL,
    revision INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
    UNIQUE(asr_session_id, asr_speaker_label), UNIQUE(source_utterance_id),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(source_utterance_id) REFERENCES utterances(id) ON DELETE RESTRICT,
    FOREIGN KEY(automatic_participant_id) REFERENCES meeting_participants(id) ON DELETE RESTRICT,
    FOREIGN KEY(speaker_cluster_id) REFERENCES speaker_clusters(id) ON DELETE RESTRICT
);
INSERT INTO speaker_tracks_v16 SELECT id, meeting_id, asr_session_id, source, asr_speaker_label,
    source_utterance_id, display_no, state, automatic_participant_id, speaker_cluster_id,
    top_score, runner_up_score, evidence_duration_ms, model_id, model_version, model_sha256,
    dimension, embedding, profile_id, last_error_code, revision, created_at, updated_at FROM speaker_tracks;
DROP TABLE speaker_tracks;
ALTER TABLE speaker_tracks_v16 RENAME TO speaker_tracks;
CREATE INDEX idx_speaker_tracks_meeting_id ON speaker_tracks(meeting_id);
CREATE INDEX idx_speaker_tracks_asr_session_id ON speaker_tracks(asr_session_id);
CREATE INDEX idx_speaker_tracks_automatic_participant_id ON speaker_tracks(automatic_participant_id);
CREATE INDEX idx_speaker_tracks_speaker_cluster_id ON speaker_tracks(speaker_cluster_id);
CREATE UNIQUE INDEX idx_speaker_tracks_meeting_display_no ON speaker_tracks(meeting_id, display_no);
