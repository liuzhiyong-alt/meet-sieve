-- Step 2 为已有声纹样本补齐来源、处理、质量和规范化格式字段。
-- SQLite 不支持为已有 CHECK 添加列，故以可恢复的表重建方式升级。
PRAGMA foreign_keys = OFF;

CREATE TABLE voice_samples_step2 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    member_id TEXT NOT NULL,
    relative_path TEXT NOT NULL UNIQUE CHECK(relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0),
    duration_ms INTEGER NOT NULL CHECK(duration_ms > 0),
    sample_rate INTEGER NOT NULL CHECK(sample_rate = 16000),
    channels INTEGER NOT NULL CHECK(channels = 1),
    bit_depth INTEGER NOT NULL CHECK(bit_depth = 16),
    size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
    sha256 TEXT NOT NULL CHECK(length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    source_kind TEXT NOT NULL CHECK(source_kind IN ('recorded', 'imported')),
    source_name TEXT NULL CHECK(source_name IS NULL OR (
        trim(source_name) <> '' AND instr(source_name, '/') = 0 AND instr(source_name, '\\') = 0
    )),
    environment_kind TEXT NOT NULL CHECK(environment_kind IN ('quiet', 'meeting_room', 'other')),
    processing_state TEXT NOT NULL CHECK(processing_state IN ('processing', 'ready', 'failed')),
    quality_state TEXT NOT NULL CHECK(quality_state IN ('pending', 'accepted', 'rejected')),
    quality_code TEXT NULL,
    quality_metrics_json TEXT NULL CHECK(quality_metrics_json IS NULL OR json_valid(quality_metrics_json)),
    last_error_code TEXT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    CHECK(processing_state <> 'ready' OR quality_state IN ('accepted', 'rejected')),
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE CASCADE
);

INSERT INTO voice_samples_step2 (
    id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256,
    source_kind, source_name, environment_kind, processing_state, quality_state, quality_code,
    quality_metrics_json, last_error_code, created_at, updated_at
)
SELECT
    id, member_id, relative_path, duration_ms, 16000, 1, 16, size_bytes, sha256,
    'imported', NULL, 'other', 'processing', 'pending', NULL, NULL, NULL, created_at, updated_at
FROM voice_samples;

CREATE TABLE voice_embeddings_step2 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    voice_sample_id TEXT NOT NULL,
    model_id TEXT NOT NULL CHECK(trim(model_id) <> ''),
    model_version TEXT NOT NULL CHECK(trim(model_version) <> ''),
    model_sha256 TEXT NOT NULL CHECK(length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*'),
    dimension INTEGER NOT NULL CHECK(dimension > 0),
    embedding BLOB NOT NULL CHECK(length(embedding) = dimension * 4),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(voice_sample_id, model_id, model_version, model_sha256, dimension),
    FOREIGN KEY(voice_sample_id) REFERENCES voice_samples_step2(id) ON DELETE CASCADE
);

INSERT INTO voice_embeddings_step2 (
    id, voice_sample_id, model_id, model_version, model_sha256, dimension, embedding, created_at, updated_at
)
SELECT id, voice_sample_id, model_id, model_version, model_sha256, dimension, embedding, created_at, updated_at
FROM voice_embeddings
WHERE length(embedding) = dimension * 4;

DROP TABLE voice_embeddings;
DROP TABLE voice_samples;
ALTER TABLE voice_samples_step2 RENAME TO voice_samples;
ALTER TABLE voice_embeddings_step2 RENAME TO voice_embeddings;

CREATE INDEX idx_voice_samples_member_quality ON voice_samples(member_id, quality_state);
CREATE INDEX idx_voice_samples_processing_updated_at ON voice_samples(processing_state, updated_at);
CREATE INDEX idx_voice_embeddings_voice_sample_id ON voice_embeddings(voice_sample_id);

PRAGMA foreign_keys = ON;
