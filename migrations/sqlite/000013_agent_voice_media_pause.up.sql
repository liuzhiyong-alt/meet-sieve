-- Step 8：记录语音指令对 ASR final 的消费关系，以及 AI 回答期间被丢弃的媒体区间。
CREATE TABLE agent_voice_command_utterances (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    command_id TEXT NOT NULL CHECK(trim(command_id) <> ''),
    utterance_id TEXT NOT NULL UNIQUE,
    agent_turn_id TEXT NULL,
    position INTEGER NOT NULL CHECK(position >= 0),
    state TEXT NOT NULL CHECK(state IN ('candidate', 'consumed', 'released')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(utterance_id) REFERENCES utterances(id) ON DELETE RESTRICT,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    UNIQUE(command_id, position)
);

CREATE INDEX idx_agent_voice_command_meeting_state
    ON agent_voice_command_utterances(meeting_id, command_id, state, position);
CREATE INDEX idx_agent_voice_command_turn
    ON agent_voice_command_utterances(agent_turn_id);

CREATE TABLE meeting_media_pauses (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    agent_turn_id TEXT NOT NULL UNIQUE,
    reason TEXT NOT NULL CHECK(reason = 'agent_voice_turn'),
    state TEXT NOT NULL CHECK(state IN ('pausing', 'paused', 'resuming', 'completed', 'failed')),
    logical_sample INTEGER NULL CHECK(logical_sample IS NULL OR logical_sample >= 0),
    physical_start_sample INTEGER NULL CHECK(physical_start_sample IS NULL OR physical_start_sample >= 0),
    physical_end_sample INTEGER NULL CHECK(physical_end_sample IS NULL OR physical_end_sample >= physical_start_sample),
    discarded_samples INTEGER NOT NULL DEFAULT 0 CHECK(discarded_samples >= 0),
    started_at INTEGER NOT NULL CHECK(started_at >= 0),
    ended_at INTEGER NULL CHECK(ended_at IS NULL OR ended_at >= started_at),
    last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT
);

CREATE INDEX idx_meeting_media_pauses_timeline
    ON meeting_media_pauses(meeting_id, logical_sample, started_at, id);
