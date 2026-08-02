-- Step 3：数据库级保证同一工作目录只有一场准备、录音或收尾中的会议。
-- 终态会议不受此索引限制，避免以应用内互斥锁替代跨重启的持久化约束。
CREATE UNIQUE INDEX idx_meetings_single_active
ON meetings ((1))
WHERE lifecycle_state IN ('preparing', 'recording', 'finalizing');
