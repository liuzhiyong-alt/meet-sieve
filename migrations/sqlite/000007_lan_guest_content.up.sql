-- Step 6：为访客消息和资源增加会话内幂等键；旧数据保持 NULL。
PRAGMA foreign_keys = OFF;

CREATE TABLE messages_step6 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    author_kind TEXT NOT NULL CHECK(author_kind IN ('host', 'guest')),
    member_id TEXT NULL,
    guest_session_id TEXT NULL,
    request_id TEXT NULL CHECK(request_id IS NULL OR length(request_id) = 36),
    display_name_snapshot TEXT NOT NULL CHECK(trim(display_name_snapshot) <> ''),
    content TEXT NOT NULL CHECK(trim(content) <> ''),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(guest_session_id, request_id),
    CHECK((author_kind = 'guest' AND guest_session_id IS NOT NULL AND member_id IS NULL) OR (author_kind = 'host' AND guest_session_id IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE RESTRICT,
    FOREIGN KEY(guest_session_id) REFERENCES guest_sessions(id) ON DELETE RESTRICT
);
INSERT INTO messages_step6 (
    id, meeting_id, event_id, author_kind, member_id, guest_session_id, request_id,
    display_name_snapshot, content, created_at, updated_at
)
SELECT id, meeting_id, event_id, author_kind, member_id, guest_session_id, NULL,
       display_name_snapshot, content, created_at, updated_at
FROM messages;
DROP TABLE messages;
ALTER TABLE messages_step6 RENAME TO messages;
CREATE INDEX idx_messages_meeting_created_at ON messages(meeting_id, created_at);
CREATE INDEX idx_messages_member_id ON messages(member_id);
CREATE INDEX idx_messages_guest_session_id ON messages(guest_session_id);

CREATE TABLE resources_step6 (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    guest_session_id TEXT NULL,
    request_id TEXT NULL CHECK(request_id IS NULL OR length(request_id) = 36),
    kind TEXT NOT NULL CHECK(kind IN ('link', 'attachment')),
    original_name TEXT NULL,
    safe_name TEXT NULL,
    relative_path TEXT NULL CHECK(relative_path IS NULL OR (relative_path NOT LIKE '/%' AND instr(relative_path, '..') = 0)),
    source_url TEXT NULL,
    media_type TEXT NULL,
    size_bytes INTEGER NULL CHECK(size_bytes IS NULL OR size_bytes >= 0),
    sha256 TEXT NULL CHECK(sha256 IS NULL OR (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*')),
    original_description TEXT NULL,
    current_description TEXT NULL,
    description_revision INTEGER NOT NULL DEFAULT 1 CHECK(description_revision >= 1),
    state TEXT NOT NULL CHECK(state IN ('ready', 'uploading', 'processing', 'completed', 'cancelled', 'failed')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(guest_session_id, request_id),
    CHECK((kind = 'link' AND source_url IS NOT NULL AND relative_path IS NULL) OR (kind = 'attachment' AND relative_path IS NOT NULL AND source_url IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(guest_session_id) REFERENCES guest_sessions(id) ON DELETE RESTRICT
);
INSERT INTO resources_step6 (
    id, meeting_id, event_id, guest_session_id, request_id, kind, original_name, safe_name,
    relative_path, source_url, media_type, size_bytes, sha256, original_description,
    current_description, description_revision, state, created_at, updated_at
)
SELECT id, meeting_id, event_id, guest_session_id, NULL, kind, original_name, safe_name,
       relative_path, source_url, media_type, size_bytes, sha256, original_description,
       current_description, description_revision, state, created_at, updated_at
FROM resources;
DROP TABLE resources;
ALTER TABLE resources_step6 RENAME TO resources;
CREATE INDEX idx_resources_meeting_state ON resources(meeting_id, state);
CREATE INDEX idx_resources_guest_session_id ON resources(guest_session_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
