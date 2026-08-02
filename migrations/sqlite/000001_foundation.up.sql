CREATE TABLE IF NOT EXISTS app_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
