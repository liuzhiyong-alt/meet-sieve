-- Step 7 回退仅用于开发临时库；删除新失败事件及无法映射回旧约束的快照事实。
PRAGMA foreign_keys = OFF;

DROP INDEX IF EXISTS idx_agent_sessions_active_meeting;
DROP INDEX IF EXISTS idx_agent_turns_active_meeting;
DROP INDEX IF EXISTS idx_agent_turns_meeting_state_created_at;

DELETE FROM meeting_events WHERE kind = 'ai.failed';
CREATE TABLE meeting_events_step6 (
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
INSERT INTO meeting_events_step6 (
    id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id,
    payload_json, created_at, updated_at
)
SELECT id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id,
       payload_json, created_at, updated_at
FROM meeting_events;
DROP TABLE meeting_events;
ALTER TABLE meeting_events_step6 RENAME TO meeting_events;
CREATE INDEX idx_meeting_events_meeting_occurred_at ON meeting_events(meeting_id, occurred_at);
CREATE INDEX idx_meeting_events_entity ON meeting_events(entity_type, entity_id);

DELETE FROM context_snapshots WHERE through_seq = 0;
DELETE FROM context_snapshots
WHERE rowid NOT IN (
    SELECT max(rowid) FROM context_snapshots GROUP BY meeting_id, through_seq
);
CREATE TABLE context_snapshots_step6 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    agent_session_id TEXT NOT NULL,
    agent_turn_id TEXT NOT NULL,
    through_seq INTEGER NOT NULL CHECK(through_seq >= 1),
    content_json TEXT NOT NULL CHECK(json_valid(content_json)),
    content_sha256 TEXT NOT NULL CHECK(length(content_sha256) = 64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, through_seq),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_session_id) REFERENCES agent_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT
);
INSERT INTO context_snapshots_step6 (
    id, meeting_id, agent_session_id, agent_turn_id, through_seq,
    content_json, content_sha256, created_at, updated_at
)
SELECT id, meeting_id, agent_session_id, agent_turn_id, through_seq,
       content_json, content_sha256, created_at, updated_at
FROM context_snapshots;
DROP TABLE context_snapshots;
ALTER TABLE context_snapshots_step6 RENAME TO context_snapshots;
CREATE INDEX idx_context_snapshots_agent_session_created_at ON context_snapshots(agent_session_id, created_at);
CREATE INDEX idx_context_snapshots_agent_turn_id ON context_snapshots(agent_turn_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
