-- 仅供开发测试回退：移除 Step 3 的单活动会议约束，不改写既有会议数据。
DROP INDEX IF EXISTS idx_meetings_single_active;
