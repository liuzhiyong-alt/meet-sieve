-- 仅供开发测试回退；丢弃 Step 2 扩展字段，保留可由 Step 1 表达的样本与向量。
PRAGMA foreign_keys = OFF;

CREATE TABLE voice_samples_step1 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    member_id TEXT NOT NULL,
    relative_path TEXT NOT NULL UNIQUE CHECK(relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0),
    duration_ms INTEGER NOT NULL CHECK(duration_ms > 0),
    sample_rate INTEGER NOT NULL CHECK(sample_rate > 0),
    size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
    sha256 TEXT NOT NULL CHECK(length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    quality_state TEXT NOT NULL CHECK(quality_state IN ('pending', 'accepted', 'rejected')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE CASCADE
);

INSERT INTO voice_samples_step1 (
    id, member_id, relative_path, duration_ms, sample_rate, size_bytes, sha256, quality_state, created_at, updated_at
)
SELECT id, member_id, relative_path, duration_ms, sample_rate, size_bytes, sha256, quality_state, created_at, updated_at
FROM voice_samples;

CREATE TABLE voice_embeddings_step1 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    voice_sample_id TEXT NOT NULL,
    model_id TEXT NOT NULL CHECK(trim(model_id) <> ''),
    model_version TEXT NOT NULL CHECK(trim(model_version) <> ''),
    model_sha256 TEXT NOT NULL CHECK(length(model_sha256) = 64 AND model_sha256 NOT GLOB '*[^0-9a-f]*'),
    dimension INTEGER NOT NULL CHECK(dimension > 0),
    embedding BLOB NOT NULL,
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(voice_sample_id, model_id, model_version, model_sha256),
    FOREIGN KEY(voice_sample_id) REFERENCES voice_samples_step1(id) ON DELETE CASCADE
);

INSERT INTO voice_embeddings_step1 (
    id, voice_sample_id, model_id, model_version, model_sha256, dimension, embedding, created_at, updated_at
)
SELECT id, voice_sample_id, model_id, model_version, model_sha256, dimension, embedding, created_at, updated_at
FROM voice_embeddings;

DROP TABLE voice_embeddings;
DROP TABLE voice_samples;
ALTER TABLE voice_samples_step1 RENAME TO voice_samples;
ALTER TABLE voice_embeddings_step1 RENAME TO voice_embeddings;

CREATE INDEX idx_voice_samples_member_quality ON voice_samples(member_id, quality_state);
CREATE INDEX idx_voice_embeddings_voice_sample_id ON voice_embeddings(voice_sample_id);

PRAGMA foreign_keys = ON;
