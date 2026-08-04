package query

// MeetingStatus 表示会议列表唯一的最高优先级展示状态。
type MeetingStatus string

const (
	// StatusDeleting 表示删除任务存在或失败，具有最高展示优先级。
	StatusDeleting MeetingStatus = "deleting"
	// StatusRecoveryRequired 表示中断会议本地保存尚未完成。
	StatusRecoveryRequired MeetingStatus = "recovery_required"
	// StatusGapConflict 表示补转写存在待人工处理冲突。
	StatusGapConflict MeetingStatus = "gap_conflict"
	// StatusGapPending 表示补转写仍待处理或失败。
	StatusGapPending MeetingStatus = "gap_pending"
	// StatusMinuteCandidate 表示存在未确认纪要草稿。
	StatusMinuteCandidate MeetingStatus = "minute_candidate"
	// StatusAgentUnsynced 表示 Codex 结束同步可重试。
	StatusAgentUnsynced MeetingStatus = "agent_unsynced"
	// StatusMinuteConfirmed 表示会议纪要已经确认。
	StatusMinuteConfirmed MeetingStatus = "minute_confirmed"
	// StatusSaved 表示本地保存完成且没有更高优先级状态。
	StatusSaved MeetingStatus = "saved"
	// StatusUnknown 表示没有匹配任何已登记展示状态。
	StatusUnknown MeetingStatus = "unknown"
)

// IsValidStatusFilter 校验列表状态筛选是否属于固定登记集合。
func IsValidStatusFilter(status string) bool {
	switch MeetingStatus(status) {
	case "", StatusDeleting, StatusRecoveryRequired, StatusGapConflict, StatusGapPending,
		StatusMinuteCandidate, StatusAgentUnsynced, StatusMinuteConfirmed, StatusSaved:
		return true
	default:
		return false
	}
}

// MeetingStatusFacts 保存列表状态投影需要的正交事实。
type MeetingStatusFacts struct {
	Deleting        bool
	LocalSaveFailed bool
	GapConflict     bool
	GapProcessing   bool
	MinuteCandidate bool
	AgentUnsynced   bool
	MinuteConfirmed bool
	LocalSaved      bool
}

type meetingStatusRule struct {
	status  MeetingStatus
	matches func(MeetingStatusFacts) bool
}

var meetingStatusRules = []meetingStatusRule{
	{status: StatusDeleting, matches: func(facts MeetingStatusFacts) bool { return facts.Deleting }},
	{status: StatusRecoveryRequired, matches: func(facts MeetingStatusFacts) bool { return facts.LocalSaveFailed }},
	{status: StatusGapConflict, matches: func(facts MeetingStatusFacts) bool { return facts.GapConflict }},
	{status: StatusGapPending, matches: func(facts MeetingStatusFacts) bool { return facts.GapProcessing }},
	{status: StatusMinuteCandidate, matches: func(facts MeetingStatusFacts) bool { return facts.MinuteCandidate }},
	{status: StatusAgentUnsynced, matches: func(facts MeetingStatusFacts) bool { return facts.AgentUnsynced }},
	{status: StatusMinuteConfirmed, matches: func(facts MeetingStatusFacts) bool { return facts.MinuteConfirmed }},
	{status: StatusSaved, matches: func(facts MeetingStatusFacts) bool { return facts.LocalSaved }},
}

// HighestPriorityStatus 按已确认的固定顺序返回唯一列表状态。
func HighestPriorityStatus(facts MeetingStatusFacts) MeetingStatus {
	for _, rule := range meetingStatusRules {
		if rule.matches(facts) {
			return rule.status
		}
	}
	return StatusUnknown
}

// ContinuationStatusesByPriority 返回首页可续办状态的固定优先级副本。
func ContinuationStatusesByPriority() []MeetingStatus {
	statuses := make([]MeetingStatus, 0, len(meetingStatusRules)-1)
	for _, rule := range meetingStatusRules {
		if rule.status == StatusSaved {
			break
		}
		statuses = append(statuses, rule.status)
	}
	return statuses
}
