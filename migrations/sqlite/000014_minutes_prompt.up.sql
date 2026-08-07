-- 会议纪要要求由用户在设置页维护；NULL 表示继续使用应用内置默认要求。
ALTER TABLE settings ADD COLUMN minute_prompt TEXT NULL
    CHECK(
        minute_prompt IS NULL OR (
            length(trim(minute_prompt)) > 0 AND
            length(CAST(minute_prompt AS BLOB)) <= 20000
        )
    );
