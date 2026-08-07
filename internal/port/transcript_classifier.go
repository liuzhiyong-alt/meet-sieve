package port

import "context"

// TranscriptFinalCandidate 是持久化前可供语音指令分类的 final 最小事实。
type TranscriptFinalCandidate struct {
	UtteranceID string
	MeetingID   string
	Text        string
	StartSample int64
	EndSample   int64
}

// TranscriptFinalClassification 描述 final 是否属于当前语音指令候选集合。
type TranscriptFinalClassification struct {
	Token     string
	CommandID string
	Position  int
	Candidate bool
}

// TranscriptFinalClassifier 在 final 事务前准备分类，并在事务结果后提交或回滚内存状态。
// PrepareFinal 到 CommitFinal/RollbackFinal 之间必须保持同一会议 final 串行。
type TranscriptFinalClassifier interface {
	PrepareFinal(ctx context.Context, candidate TranscriptFinalCandidate) TranscriptFinalClassification
	CommitFinal(token string)
	RollbackFinal(token string)
}
