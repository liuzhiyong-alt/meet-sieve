-- 为会议游标查询和资源打开前的完整性校验补充必要结构。
CREATE INDEX idx_meetings_started_no
    ON meetings(started_at DESC, meeting_no DESC);

CREATE INDEX idx_meeting_participants_snapshot_meeting
    ON meeting_participants(display_name_snapshot, meeting_id);

ALTER TABLE resources ADD COLUMN integrity_state TEXT NOT NULL DEFAULT 'unchecked'
    CHECK(integrity_state IN ('unchecked', 'verified', 'missing', 'changed', 'outside_workspace', 'unavailable'));
ALTER TABLE resources ADD COLUMN last_verified_at INTEGER NULL
    CHECK(last_verified_at IS NULL OR last_verified_at >= 0);
ALTER TABLE resources ADD COLUMN integrity_error_code TEXT NULL;
