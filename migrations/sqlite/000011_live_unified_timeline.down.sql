-- 仅供开发测试回退；正式恢复使用升级前备份。
PRAGMA foreign_keys = OFF;

DROP INDEX IF EXISTS idx_resources_host_request;

CREATE TABLE messages_step10_rollback (
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

INSERT INTO messages_step10_rollback (
    id, meeting_id, event_id, author_kind, member_id, guest_session_id, request_id,
    display_name_snapshot, content, created_at, updated_at
)
SELECT id, meeting_id, event_id, author_kind, member_id, guest_session_id, request_id,
       display_name_snapshot, content, created_at, updated_at
FROM messages;

DROP TABLE messages;
ALTER TABLE messages_step10_rollback RENAME TO messages;

CREATE INDEX idx_messages_meeting_created_at ON messages(meeting_id, created_at);
CREATE INDEX idx_messages_member_id ON messages(member_id);
CREATE INDEX idx_messages_guest_session_id ON messages(guest_session_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
