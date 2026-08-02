-- 仅供开发测试的破坏性回退：删除 Step 1 数据，不恢复任何 legacy key/value。
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS app_metadata;
DROP TABLE IF EXISTS deletion_jobs;
DROP TABLE IF EXISTS minute_versions;
DROP TABLE IF EXISTS context_snapshots;
DROP TABLE IF EXISTS sync_batches;
DROP TABLE IF EXISTS agent_turns;
DROP TABLE IF EXISTS agent_sessions;
DROP TABLE IF EXISTS speaker_clusters;
DROP TABLE IF EXISTS voice_embeddings;
DROP TABLE IF EXISTS voice_samples;
DROP TABLE IF EXISTS asr_gaps;
DROP TABLE IF EXISTS asr_sessions;
DROP TABLE IF EXISTS audio_assets;
DROP TABLE IF EXISTS corrections;
DROP TABLE IF EXISTS resources;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS guest_sessions;
DROP TABLE IF EXISTS utterances;
DROP TABLE IF EXISTS meeting_events;
DROP TABLE IF EXISTS meeting_participants;
DROP TABLE IF EXISTS meetings;
DROP TABLE IF EXISTS meeting_number_sequences;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS members;
DROP TABLE IF EXISTS app_metadata_legacy;

-- 恢复空的 Step 0 占位结构，生产运行不得执行此 down migration。
CREATE TABLE app_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
