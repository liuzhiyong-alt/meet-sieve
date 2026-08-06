-- provider label 仅作为通道线索；同一通道允许多个本地声学 segment。
CREATE TABLE speaker_tracks_v17 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    asr_session_id TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'provider_label' CHECK(source IN ('provider_label', 'local_utterance')),
    asr_speaker_label TEXT NULL CHECK(asr_speaker_label IS NULL OR trim(asr_speaker_label) <> ''),
    provider_segment_no INTEGER NULL CHECK(provider_segment_no IS NULL OR provider_segment_no >= 1),
    source_utterance_id TEXT NULL,
    display_no INTEGER NULL CHECK(display_no IS NULL OR display_no >= 1),
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
    routing_revision INTEGER NOT NULL DEFAULT 1 CHECK(routing_revision >= 1),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision >= 1),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(asr_session_id, asr_speaker_label, provider_segment_no),
    UNIQUE(source_utterance_id),
    CHECK((source = 'provider_label' AND asr_speaker_label IS NOT NULL AND provider_segment_no IS NOT NULL AND source_utterance_id IS NULL) OR
          (source = 'local_utterance' AND asr_speaker_label IS NULL AND provider_segment_no IS NULL AND source_utterance_id IS NOT NULL)),
    CHECK((embedding IS NULL AND model_id IS NULL AND model_version IS NULL AND model_sha256 IS NULL AND dimension IS NULL AND profile_id IS NULL) OR
          (embedding IS NOT NULL AND model_id IS NOT NULL AND model_version IS NOT NULL AND model_sha256 IS NOT NULL AND dimension IS NOT NULL AND profile_id IS NOT NULL AND length(embedding) = dimension * 4)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(source_utterance_id) REFERENCES utterances(id) ON DELETE RESTRICT,
    FOREIGN KEY(automatic_participant_id) REFERENCES meeting_participants(id) ON DELETE RESTRICT,
    FOREIGN KEY(speaker_cluster_id) REFERENCES speaker_clusters(id) ON DELETE RESTRICT
);

INSERT INTO speaker_tracks_v17(
    id, meeting_id, asr_session_id, source, asr_speaker_label, provider_segment_no, source_utterance_id,
    display_no, state, automatic_participant_id, speaker_cluster_id, top_score, runner_up_score,
    evidence_duration_ms, model_id, model_version, model_sha256, dimension, embedding, profile_id,
    last_error_code, routing_revision, revision, created_at, updated_at
)
SELECT id, meeting_id, asr_session_id, source, asr_speaker_label,
       CASE WHEN source = 'provider_label' THEN 1 ELSE NULL END, source_utterance_id,
       display_no, state, automatic_participant_id, speaker_cluster_id, top_score, runner_up_score,
       evidence_duration_ms, model_id, model_version, model_sha256, dimension, embedding, profile_id,
       last_error_code, 1, revision, created_at, updated_at
FROM speaker_tracks;

DROP TABLE speaker_tracks;
ALTER TABLE speaker_tracks_v17 RENAME TO speaker_tracks;
CREATE INDEX idx_speaker_tracks_meeting_id ON speaker_tracks(meeting_id);
CREATE INDEX idx_speaker_tracks_asr_session_id ON speaker_tracks(asr_session_id);
CREATE INDEX idx_speaker_tracks_automatic_participant_id ON speaker_tracks(automatic_participant_id);
CREATE INDEX idx_speaker_tracks_speaker_cluster_id ON speaker_tracks(speaker_cluster_id);
CREATE UNIQUE INDEX idx_speaker_tracks_meeting_display_no ON speaker_tracks(meeting_id, display_no);

ALTER TABLE speaker_track_evidence ADD COLUMN routing_state TEXT NOT NULL DEFAULT 'routed'
    CHECK(routing_state IN ('pending', 'routed', 'insufficient', 'failed'));
ALTER TABLE speaker_track_evidence ADD COLUMN routing_error_code TEXT NULL;
ALTER TABLE speaker_track_evidence ADD COLUMN routing_duration_ms INTEGER NOT NULL DEFAULT 0
    CHECK(routing_duration_ms >= 0);
ALTER TABLE speaker_track_evidence ADD COLUMN routing_score REAL NULL
    CHECK(routing_score IS NULL OR (routing_score >= -1 AND routing_score <= 1));
ALTER TABLE speaker_track_evidence ADD COLUMN routing_margin REAL NULL
    CHECK(routing_margin IS NULL OR (routing_margin >= 0 AND routing_margin <= 2));
ALTER TABLE speaker_track_evidence ADD COLUMN routing_model_id TEXT NULL;
ALTER TABLE speaker_track_evidence ADD COLUMN routing_model_version TEXT NULL;
ALTER TABLE speaker_track_evidence ADD COLUMN routing_model_sha256 TEXT NULL;
ALTER TABLE speaker_track_evidence ADD COLUMN routing_dimension INTEGER NULL;
ALTER TABLE speaker_track_evidence ADD COLUMN routing_profile_id TEXT NULL;
ALTER TABLE speaker_track_evidence ADD COLUMN routing_embedding BLOB NULL;
CREATE INDEX idx_speaker_evidence_routing_state ON speaker_track_evidence(routing_state, created_at);
