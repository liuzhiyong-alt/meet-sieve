-- 回滚仅支持已有 provider 标签 track；本地候选 track 没有可逆的 provider 标签。
DELETE FROM speaker_tracks WHERE source = 'local_utterance';

CREATE TABLE speaker_tracks_v15 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    asr_session_id TEXT NOT NULL,
    asr_speaker_label TEXT NOT NULL CHECK(trim(asr_speaker_label) <> ''),
    display_no INTEGER NULL CHECK(display_no IS NULL OR display_no >= 1),
    state TEXT NOT NULL CHECK(state IN ('collecting', 'pending', 'matched', 'clustered', 'ambiguous', 'insufficient', 'unavailable', 'failed', 'rebuild_required')),
    automatic_participant_id TEXT NULL, speaker_cluster_id TEXT NULL,
    top_score REAL NULL, runner_up_score REAL NULL, evidence_duration_ms INTEGER NOT NULL DEFAULT 0,
    model_id TEXT NULL, model_version TEXT NULL, model_sha256 TEXT NULL, dimension INTEGER NULL,
    embedding BLOB NULL, profile_id TEXT NULL, last_error_code TEXT NULL,
    revision INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
INSERT INTO speaker_tracks_v15 SELECT id, meeting_id, asr_session_id, asr_speaker_label, display_no, state,
    automatic_participant_id, speaker_cluster_id, top_score, runner_up_score, evidence_duration_ms,
    model_id, model_version, model_sha256, dimension, embedding, profile_id, last_error_code,
    revision, created_at, updated_at FROM speaker_tracks;
DROP TABLE speaker_tracks;
ALTER TABLE speaker_tracks_v15 RENAME TO speaker_tracks;
CREATE UNIQUE INDEX idx_speaker_tracks_meeting_display_no ON speaker_tracks(meeting_id, display_no);
