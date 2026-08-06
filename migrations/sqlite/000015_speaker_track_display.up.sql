-- 为 ASR 匿名 track 分配会议内稳定展示编号；该编号不代表成员身份。
ALTER TABLE speaker_tracks
    ADD COLUMN display_no INTEGER NULL CHECK(display_no IS NULL OR display_no >= 1);

-- 旧数据按首次创建顺序确定性回填，UUID 只用于同毫秒时稳定排序。
WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY meeting_id ORDER BY created_at ASC, id ASC) AS display_no
    FROM speaker_tracks
)
UPDATE speaker_tracks
SET display_no = (SELECT ranked.display_no FROM ranked WHERE ranked.id = speaker_tracks.id);

CREATE UNIQUE INDEX idx_speaker_tracks_meeting_display_no
    ON speaker_tracks(meeting_id, display_no);
