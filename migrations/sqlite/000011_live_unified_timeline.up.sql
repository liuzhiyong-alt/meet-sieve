-- 会中统一时间线：为新消息登记渲染格式，并为主持人写入增加会议内幂等约束。
PRAGMA foreign_keys = OFF;

CREATE TABLE messages_live_timeline (
    id TEXT PRIMARY KEY CHECK(length(id) = 36),
    meeting_id TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    author_kind TEXT NOT NULL CHECK(author_kind IN ('host', 'guest')),
    member_id TEXT NULL,
    guest_session_id TEXT NULL,
    request_id TEXT NULL CHECK(request_id IS NULL OR length(request_id) = 36),
    display_name_snapshot TEXT NOT NULL CHECK(trim(display_name_snapshot) <> ''),
    content TEXT NOT NULL CHECK(trim(content) <> ''),
    content_format TEXT NOT NULL DEFAULT 'plain' CHECK(content_format IN ('plain', 'markdown')),
    created_at INTEGER NOT NULL CHECK(created_at >= 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    UNIQUE(guest_session_id, request_id),
    CHECK((author_kind = 'guest' AND guest_session_id IS NOT NULL AND member_id IS NULL) OR (author_kind = 'host' AND guest_session_id IS NULL)),
    FOREIGN KEY(meeting_id) REFERENCES meetings(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES meeting_events(id) ON DELETE RESTRICT,
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE RESTRICT,
    FOREIGN KEY(guest_session_id) REFERENCES guest_sessions(id) ON DELETE RESTRICT
);

INSERT INTO messages_live_timeline (
    id, meeting_id, event_id, author_kind, member_id, guest_session_id, request_id,
    display_name_snapshot, content, content_format, created_at, updated_at
)
SELECT id, meeting_id, event_id, author_kind, member_id, guest_session_id, request_id,
       display_name_snapshot, content, 'plain', created_at, updated_at
FROM messages;

DROP TABLE messages;
ALTER TABLE messages_live_timeline RENAME TO messages;

CREATE INDEX idx_messages_meeting_created_at ON messages(meeting_id, created_at);
CREATE INDEX idx_messages_member_id ON messages(member_id);
CREATE INDEX idx_messages_guest_session_id ON messages(guest_session_id);
CREATE UNIQUE INDEX idx_messages_host_request
    ON messages(meeting_id, request_id)
    WHERE author_kind = 'host' AND request_id IS NOT NULL;

CREATE UNIQUE INDEX idx_resources_host_request
    ON resources(meeting_id, request_id)
    WHERE guest_session_id IS NULL AND request_id IS NOT NULL;

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
