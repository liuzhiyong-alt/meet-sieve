-- Step 1 将未发布的 Step 0 key/value 占位 metadata 保留为 legacy，随后创建 typed singleton。
ALTER TABLE app_metadata RENAME TO app_metadata_legacy;

-- 应用身份元数据；具体 UUID、设备码和创建版本由 FoundationFinalizer 在独立事务写入。
CREATE TABLE app_metadata (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    singleton_key INTEGER NOT NULL UNIQUE CHECK(singleton_key = 1),
    product TEXT NOT NULL CHECK(product = 'meet-sieve'),
    database_id TEXT NOT NULL UNIQUE CHECK(length(database_id) = 36),
    device_code TEXT NOT NULL CHECK(device_code GLOB '[A-HJ-NP-Z2-9][A-HJ-NP-Z2-9][A-HJ-NP-Z2-9][A-HJ-NP-Z2-9]'),
    created_with_app_version TEXT NOT NULL CHECK(trim(created_with_app_version) <> ''),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at)
);

-- 用户业务设置 singleton；凭据允许为 NULL，鉴权模式只允许已确认的两种值。
CREATE TABLE settings (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    singleton_key INTEGER NOT NULL UNIQUE CHECK(singleton_key = 1),
    volc_auth_mode TEXT NULL CHECK(volc_auth_mode IS NULL OR volc_auth_mode IN ('legacy', 'api_key')),
    volc_api_app_key TEXT NULL,
    volc_api_access_key TEXT NULL,
    volc_api_key TEXT NULL,
    default_microphone_id TEXT NULL,
    wake_word TEXT NOT NULL DEFAULT 'AI 助手' CHECK(trim(wake_word) <> ''),
    codex_executable_path TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at)
);

-- 成员支持归档；活动成员名称规范化后唯一，历史会议引用由后续表使用 RESTRICT 保护。
CREATE TABLE members (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    name TEXT NOT NULL CHECK(trim(name) <> ''),
    name_normalized TEXT NOT NULL CHECK(trim(name_normalized) <> ''),
    notes TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    archived_at INTEGER NULL CHECK(archived_at IS NULL OR archived_at >= created_at)
);
CREATE UNIQUE INDEX idx_members_active_name_normalized
    ON members(name_normalized) WHERE archived_at IS NULL;

-- 小组只维护当前成员关系，不改写历史参会者快照。
CREATE TABLE groups (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    name TEXT NOT NULL CHECK(trim(name) <> ''),
    name_normalized TEXT NOT NULL CHECK(trim(name_normalized) <> ''),
    default_lan_enabled INTEGER NOT NULL DEFAULT 0 CHECK(default_lan_enabled IN (0, 1)),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    archived_at INTEGER NULL CHECK(archived_at IS NULL OR archived_at >= created_at)
);
CREATE UNIQUE INDEX idx_groups_active_name_normalized
    ON groups(name_normalized) WHERE archived_at IS NULL;

-- 小组成员关系按小组内排序；两个外键均受 RESTRICT 保护。
CREATE TABLE group_members (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    group_id TEXT NOT NULL,
    member_id TEXT NOT NULL,
    sort_order INTEGER NOT NULL CHECK(sort_order >= 0),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(group_id, member_id),
    UNIQUE(group_id, sort_order),
    FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE RESTRICT,
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE RESTRICT
);
CREATE INDEX idx_group_members_group_id ON group_members(group_id);
CREATE INDEX idx_group_members_member_id ON group_members(member_id);

-- 每日设备内会议编号序列，序列分配由创建会议事务负责。
CREATE TABLE meeting_number_sequences (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    local_date TEXT NOT NULL CHECK(length(local_date) = 8 AND local_date GLOB '[0-9]*'),
    device_code TEXT NOT NULL CHECK(device_code GLOB '[A-HJ-NP-Z2-9][A-HJ-NP-Z2-9][A-HJ-NP-Z2-9][A-HJ-NP-Z2-9]'),
    next_sequence INTEGER NOT NULL CHECK(next_sequence >= 1),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(local_date, device_code)
);

-- 会议聚合保存七个正交状态轴，禁止用单一 status 混淆录音、保存、ASR、Agent、纪要和 LAN。
CREATE TABLE meetings (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_no TEXT NOT NULL UNIQUE CHECK(trim(meeting_no) <> ''),
    subject TEXT NOT NULL CHECK(trim(subject) <> ''),
    relative_dir TEXT NOT NULL UNIQUE CHECK(relative_dir NOT LIKE '/%' AND instr(relative_dir, '..') = 0),
    local_timezone TEXT NOT NULL CHECK(trim(local_timezone) <> ''),
    started_at INTEGER NULL CHECK(started_at IS NULL OR started_at >= 0),
    ended_at INTEGER NULL CHECK(ended_at IS NULL OR (started_at IS NOT NULL AND ended_at >= started_at)),
    lifecycle_state TEXT NOT NULL CHECK(lifecycle_state IN ('preparing', 'recording', 'finalizing', 'ended', 'interrupted', 'deleting', 'delete_failed')),
    local_save_state TEXT NOT NULL CHECK(local_save_state IN ('pending', 'saving', 'saved', 'failed')),
    realtime_asr_state TEXT NOT NULL CHECK(realtime_asr_state IN ('idle', 'connecting', 'streaming', 'reconnecting', 'unavailable', 'stopped')),
    gap_state TEXT NOT NULL CHECK(gap_state IN ('none', 'pending', 'processing', 'completed', 'failed', 'conflict')),
    agent_state TEXT NOT NULL CHECK(agent_state IN ('unchecked', 'initializing', 'available', 'busy', 'unavailable', 'unsynced')),
    minute_state TEXT NOT NULL CHECK(minute_state IN ('not_generated', 'generating', 'draft', 'confirmed', 'failed')),
    lan_state TEXT NOT NULL CHECK(lan_state IN ('disabled', 'starting', 'serving', 'failed', 'stopped')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at)
);
CREATE INDEX idx_meetings_lifecycle_started_at ON meetings(lifecycle_state, started_at);
CREATE INDEX idx_meetings_started_at ON meetings(started_at);

-- 参会者快照支持已登记成员和临时参与者；历史 member 引用不得被级联删除。
CREATE TABLE meeting_participants (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    member_id TEXT NULL,
    participant_kind TEXT NOT NULL CHECK(participant_kind IN ('member', 'temporary')),
    display_name_snapshot TEXT NOT NULL CHECK(trim(display_name_snapshot) <> ''),
    sort_order INTEGER NOT NULL CHECK(sort_order >= 0),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, sort_order),
    CHECK((participant_kind = 'member' AND member_id IS NOT NULL) OR (participant_kind = 'temporary' AND member_id IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE RESTRICT
);
CREATE INDEX idx_meeting_participants_meeting_id ON meeting_participants(meeting_id);
CREATE INDEX idx_meeting_participants_member_id ON meeting_participants(member_id);

-- 所有持久会议活动使用单调 seq；partial 转写不入库也不占用序号。
CREATE TABLE meeting_events (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    seq INTEGER NOT NULL CHECK(seq >= 1),
    kind TEXT NOT NULL CHECK(kind IN ('utterance.final', 'message.created', 'resource.created', 'utterance.corrected', 'speaker.corrected', 'resource.corrected', 'ai.question', 'ai.answer', 'ai.cancelled', 'asr.gap', 'asr.compensated')),
    occurred_at INTEGER NOT NULL CHECK(occurred_at >= 0),
    source TEXT NOT NULL CHECK(source IN ('host', 'guest', 'asr', 'agent', 'system')),
    entity_type TEXT NULL CHECK(entity_type IS NULL OR entity_type IN ('utterance', 'message', 'resource', 'correction', 'asr_gap', 'agent_turn', 'minute_version')),
    entity_id TEXT NULL,
    payload_json TEXT NULL CHECK(payload_json IS NULL OR json_valid(payload_json)),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, seq),
    CHECK((entity_type IS NULL AND entity_id IS NULL) OR (entity_type IS NOT NULL AND entity_id IS NOT NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
CREATE INDEX idx_meeting_events_meeting_occurred_at ON meeting_events(meeting_id, occurred_at);
CREATE INDEX idx_meeting_events_entity ON meeting_events(entity_type, entity_id);

-- 最终转写保留原始文本和当前投影文本；校正不覆盖原始 ASR 文本。
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
    current_member_id TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(asr_session_id, provider_result_id),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(current_member_id) REFERENCES members(id) ON DELETE RESTRICT
);
CREATE INDEX idx_utterances_meeting_start_sample ON utterances(meeting_id, start_sample);
CREATE INDEX idx_utterances_asr_session_id ON utterances(asr_session_id);
CREATE INDEX idx_utterances_current_member_id ON utterances(current_member_id);

-- LAN 访客会话以哈希 token 标识，过期和撤销均保留审计状态。
CREATE TABLE guest_sessions (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    display_name TEXT NOT NULL CHECK(trim(display_name) <> ''),
    session_token_hash TEXT NOT NULL UNIQUE CHECK(length(session_token_hash) = 64 AND session_token_hash NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK(state IN ('active', 'expired', 'revoked')),
    expires_at INTEGER NOT NULL CHECK(expires_at >= 0),
    last_seen_at INTEGER NULL CHECK(last_seen_at IS NULL OR last_seen_at >= 0),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
CREATE INDEX idx_guest_sessions_meeting_state ON guest_sessions(meeting_id, state);

-- 消息作者区分 host 与 guest，展示名称一律保存快照。
CREATE TABLE messages (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    author_kind TEXT NOT NULL CHECK(author_kind IN ('host', 'guest')),
    member_id TEXT NULL,
    guest_session_id TEXT NULL,
    display_name_snapshot TEXT NOT NULL CHECK(trim(display_name_snapshot) <> ''),
    content TEXT NOT NULL CHECK(trim(content) <> ''),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((author_kind = 'guest' AND guest_session_id IS NOT NULL AND member_id IS NULL) OR (author_kind = 'host' AND guest_session_id IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE RESTRICT,
    FOREIGN KEY(guest_session_id) REFERENCES guest_sessions(id) ON DELETE RESTRICT
);
CREATE INDEX idx_messages_meeting_created_at ON messages(meeting_id, created_at);
CREATE INDEX idx_messages_member_id ON messages(member_id);
CREATE INDEX idx_messages_guest_session_id ON messages(guest_session_id);

-- 会议资源只记录工作目录内相对附件路径或链接地址。
CREATE TABLE resources (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    guest_session_id TEXT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('link', 'attachment')),
    original_name TEXT NULL,
    safe_name TEXT NULL,
    relative_path TEXT NULL CHECK(relative_path IS NULL OR (relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0)),
    source_url TEXT NULL,
    media_type TEXT NULL,
    size_bytes INTEGER NULL CHECK(size_bytes IS NULL OR size_bytes >= 0),
    sha256 TEXT NULL CHECK(sha256 IS NULL OR (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*')),
    state TEXT NOT NULL CHECK(state IN ('ready', 'uploading', 'processing', 'completed', 'cancelled', 'failed')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((kind = 'link' AND source_url IS NOT NULL AND relative_path IS NULL) OR (kind = 'attachment' AND relative_path IS NOT NULL AND source_url IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(guest_session_id) REFERENCES guest_sessions(id) ON DELETE RESTRICT
);
CREATE INDEX idx_resources_meeting_state ON resources(meeting_id, state);
CREATE INDEX idx_resources_guest_session_id ON resources(guest_session_id);

-- 校正保留前后 JSON 和操作者，目标存在性及同会议关系由 service 在事务中确认。
CREATE TABLE corrections (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    target_kind TEXT NOT NULL CHECK(target_kind IN ('utterance', 'speaker_cluster', 'message', 'resource')),
    target_id TEXT NOT NULL,
    correction_kind TEXT NOT NULL CHECK(correction_kind IN ('text', 'member_assignment', 'author_assignment', 'description')),
    before_json TEXT NOT NULL CHECK(json_valid(before_json)),
    after_json TEXT NOT NULL CHECK(json_valid(after_json)),
    operator_kind TEXT NOT NULL CHECK(operator_kind IN ('host', 'guest', 'system')),
    operator_id TEXT NULL,
    reason TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((operator_kind = 'system' AND operator_id IS NULL) OR (operator_kind IN ('host', 'guest') AND operator_id IS NOT NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT
);
CREATE INDEX idx_corrections_target_created_at ON corrections(target_kind, target_id, created_at);
CREATE INDEX idx_corrections_meeting_id ON corrections(meeting_id);

-- 音频资产使用会议内序号和相对路径；完整性哈希固定为小写 SHA-256。
CREATE TABLE audio_assets (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('mixed', 'microphone', 'system', 'gap')),
    sequence_no INTEGER NOT NULL CHECK(sequence_no >= 1),
    relative_path TEXT NOT NULL CHECK(relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0),
    start_sample INTEGER NOT NULL CHECK(start_sample >= 0), end_sample INTEGER NOT NULL CHECK(end_sample > start_sample),
    sample_rate INTEGER NOT NULL CHECK(sample_rate > 0), bit_depth INTEGER NOT NULL CHECK(bit_depth > 0), channels INTEGER NOT NULL CHECK(channels > 0),
    size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0), sha256 TEXT NOT NULL CHECK(length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK(state IN ('writing', 'ready', 'failed', 'deleted')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, kind, sequence_no), FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
CREATE INDEX idx_audio_assets_meeting_id ON audio_assets(meeting_id);

-- 实时 ASR session 只接受当前确认的火山 provider，并显式记录重连次数。
CREATE TABLE asr_sessions (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK(provider = 'volcano'), provider_session_id TEXT NULL,
    state TEXT NOT NULL CHECK(state IN ('connecting', 'streaming', 'disconnected', 'stopped', 'failed')),
    started_at INTEGER NOT NULL CHECK(started_at >= 0), ended_at INTEGER NULL CHECK(ended_at IS NULL OR ended_at >= started_at),
    reconnect_count INTEGER NOT NULL DEFAULT 0 CHECK(reconnect_count >= 0), last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(provider, provider_session_id), FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
);
CREATE INDEX idx_asr_sessions_meeting_started_at ON asr_sessions(meeting_id, started_at);

-- ASR 缺口关联原始事件及可选音频/session，补转写结果序号必须成对出现。
CREATE TABLE asr_gaps (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, event_id TEXT NOT NULL UNIQUE,
    asr_session_id TEXT NULL, audio_asset_id TEXT NULL,
    start_sample INTEGER NOT NULL CHECK(start_sample >= 0), end_sample INTEGER NOT NULL CHECK(end_sample > start_sample),
    state TEXT NOT NULL CHECK(state IN ('pending', 'processing', 'completed', 'failed', 'conflict')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0), result_from_seq INTEGER NULL, result_to_seq INTEGER NULL,
    conflict_json TEXT NULL CHECK(conflict_json IS NULL OR json_valid(conflict_json)), last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK((result_from_seq IS NULL AND result_to_seq IS NULL) OR (result_from_seq IS NOT NULL AND result_to_seq IS NOT NULL AND result_from_seq >= 1 AND result_to_seq >= result_from_seq)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(audio_asset_id) REFERENCES audio_assets(id) ON DELETE RESTRICT
);
CREATE INDEX idx_asr_gaps_meeting_state ON asr_gaps(meeting_id, state);
CREATE INDEX idx_asr_gaps_asr_session_id ON asr_gaps(asr_session_id);
CREATE INDEX idx_asr_gaps_audio_asset_id ON asr_gaps(audio_asset_id);

-- 声纹样本属于成员，删除成员时级联清理样本及 embedding。
CREATE TABLE voice_samples (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), member_id TEXT NOT NULL,
    relative_path TEXT NOT NULL UNIQUE CHECK(relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0),
    duration_ms INTEGER NOT NULL CHECK(duration_ms > 0), sample_rate INTEGER NOT NULL CHECK(sample_rate > 0), size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
    sha256 TEXT NOT NULL CHECK(length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'), quality_state TEXT NOT NULL CHECK(quality_state IN ('pending', 'accepted', 'rejected')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE CASCADE
);
CREATE INDEX idx_voice_samples_member_quality ON voice_samples(member_id, quality_state);

CREATE TABLE voice_embeddings (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), voice_sample_id TEXT NOT NULL,
    model_id TEXT NOT NULL CHECK(trim(model_id) <> ''), model_version TEXT NOT NULL CHECK(trim(model_version) <> ''),
    model_sha256 TEXT NOT NULL CHECK(length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*'),
    dimension INTEGER NOT NULL CHECK(dimension > 0), embedding BLOB NOT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(voice_sample_id, model_id, model_version, model_sha256),
    FOREIGN KEY(voice_sample_id) REFERENCES voice_samples(id) ON DELETE CASCADE
);
CREATE INDEX idx_voice_embeddings_voice_sample_id ON voice_embeddings(voice_sample_id);

-- speaker label 只在一个 ASR session 内唯一，不按 meeting 全局合并重连前后标签。
CREATE TABLE speaker_clusters (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, asr_session_id TEXT NOT NULL,
    asr_speaker_label TEXT NOT NULL CHECK(trim(asr_speaker_label) <> ''), assigned_member_id TEXT NULL,
    confidence REAL NULL CHECK(confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    assignment_source TEXT NOT NULL CHECK(assignment_source IN ('automatic', 'manual', 'unassigned')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(asr_session_id, asr_speaker_label), FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(asr_session_id) REFERENCES asr_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(assigned_member_id) REFERENCES members(id) ON DELETE RESTRICT
);
CREATE INDEX idx_speaker_clusters_meeting_id ON speaker_clusters(meeting_id);
CREATE INDEX idx_speaker_clusters_asr_session_id ON speaker_clusters(asr_session_id);
CREATE INDEX idx_speaker_clusters_assigned_member_id ON speaker_clusters(assigned_member_id);

-- 每场会议的 Codex session 保存相对 cwd；恢复来源使用 SET NULL 保留历史。
CREATE TABLE agent_sessions (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK(provider = 'codex'), thread_id TEXT NULL,
    cwd_relative_path TEXT NOT NULL CHECK(cwd_relative_path NOT LIKE '/%' AND instr(cwd_relative_path, '..') = 0),
    state TEXT NOT NULL CHECK(state IN ('starting', 'available', 'unavailable', 'ended', 'failed')),
    resumed_from_session_id TEXT NULL, started_at INTEGER NOT NULL CHECK(started_at >= 0),
    ended_at INTEGER NULL CHECK(ended_at IS NULL OR ended_at >= started_at), last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(provider, thread_id), FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(resumed_from_session_id) REFERENCES agent_sessions(id) ON DELETE SET NULL
);
CREATE INDEX idx_agent_sessions_meeting_id ON agent_sessions(meeting_id);
CREATE INDEX idx_agent_sessions_resumed_from_session_id ON agent_sessions(resumed_from_session_id);

CREATE TABLE agent_turns (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, agent_session_id TEXT NOT NULL,
    provider_turn_id TEXT NULL, kind TEXT NOT NULL CHECK(kind IN ('initialize', 'answer', 'minutes', 'ingest')),
    state TEXT NOT NULL CHECK(state IN ('pending', 'running', 'completed', 'cancelled', 'failed', 'timed_out')),
    idempotency_key TEXT NOT NULL CHECK(trim(idempotency_key) <> ''), question_event_id TEXT NULL, answer_event_id TEXT NULL,
    started_at INTEGER NULL CHECK(started_at IS NULL OR started_at >= 0), ended_at INTEGER NULL CHECK(ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at), last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(agent_session_id, idempotency_key), FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_session_id) REFERENCES agent_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(question_event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(answer_event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT
);
CREATE INDEX idx_agent_turns_meeting_id ON agent_turns(meeting_id);
CREATE INDEX idx_agent_turns_agent_session_id ON agent_turns(agent_session_id);
CREATE INDEX idx_agent_turns_question_event_id ON agent_turns(question_event_id);
CREATE INDEX idx_agent_turns_answer_event_id ON agent_turns(answer_event_id);

CREATE TABLE sync_batches (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, agent_session_id TEXT NOT NULL,
    from_seq INTEGER NOT NULL CHECK(from_seq >= 1), to_seq INTEGER NOT NULL CHECK(to_seq >= from_seq),
    idempotency_key TEXT NOT NULL CHECK(trim(idempotency_key) <> ''), state TEXT NOT NULL CHECK(state IN ('pending', 'running', 'completed', 'failed')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0), last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(agent_session_id, idempotency_key), FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_session_id) REFERENCES agent_sessions(id) ON DELETE RESTRICT
);
CREATE INDEX idx_sync_batches_meeting_state ON sync_batches(meeting_id, state);
CREATE INDEX idx_sync_batches_agent_session_id ON sync_batches(agent_session_id);

CREATE TABLE context_snapshots (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, agent_session_id TEXT NOT NULL, agent_turn_id TEXT NOT NULL,
    through_seq INTEGER NOT NULL CHECK(through_seq >= 1), content_json TEXT NOT NULL CHECK(json_valid(content_json)),
    content_sha256 TEXT NOT NULL CHECK(length(content_sha256) = 64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, through_seq), FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_session_id) REFERENCES agent_sessions(id) ON DELETE RESTRICT,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT
);
CREATE INDEX idx_context_snapshots_agent_session_created_at ON context_snapshots(agent_session_id, created_at);
CREATE INDEX idx_context_snapshots_agent_turn_id ON context_snapshots(agent_turn_id);

CREATE TABLE minute_versions (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL, agent_turn_id TEXT NULL, parent_version_id TEXT NULL,
    version_no INTEGER NOT NULL CHECK(version_no >= 1), source TEXT NOT NULL CHECK(source IN ('ai', 'human', 'restored')),
    content_markdown TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('draft', 'confirmed')),
    is_current INTEGER NOT NULL DEFAULT 1 CHECK(is_current IN (0, 1)), confirmed_at INTEGER NULL CHECK(confirmed_at IS NULL OR confirmed_at >= 0),
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(meeting_id, version_no), CHECK((state = 'confirmed' AND confirmed_at IS NOT NULL) OR (state = 'draft' AND confirmed_at IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(agent_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    FOREIGN KEY(parent_version_id) REFERENCES minute_versions(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX idx_minute_versions_current_meeting ON minute_versions(meeting_id) WHERE is_current = 1;
CREATE INDEX idx_minute_versions_agent_turn_id ON minute_versions(agent_turn_id);
CREATE INDEX idx_minute_versions_parent_version_id ON minute_versions(parent_version_id);

-- 删除任务在 meeting 存在时跟踪文件处理；完成前禁止数据库级级联删除会议。
CREATE TABLE deletion_jobs (
    id TEXT PRIMARY KEY CHECK(length(id) = 36), meeting_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('recording', 'meeting')), state TEXT NOT NULL CHECK(state IN ('pending', 'running', 'completed', 'failed')),
    target_manifest_json TEXT NOT NULL CHECK(json_valid(target_manifest_json)), failed_items_json TEXT NULL CHECK(failed_items_json IS NULL OR json_valid(failed_items_json)),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0), last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0), updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX idx_deletion_jobs_active_kind ON deletion_jobs(meeting_id, kind) WHERE state IN ('pending', 'running', 'failed');
CREATE INDEX idx_deletion_jobs_meeting_id ON deletion_jobs(meeting_id);
