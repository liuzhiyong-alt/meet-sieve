package port

import "context"

// TranscriptionEventType 是实时转写事件的稳定业务类型。
type TranscriptionEventType string

const (
	// TranscriptionPartial 表示仅用于 UI 的临时文本。
	TranscriptionPartial TranscriptionEventType = "partial"
	// TranscriptionFinal 表示可以持久化的最终文本。
	TranscriptionFinal TranscriptionEventType = "final"
	// TranscriptionSessionStarted 表示远端会话已经建立。
	TranscriptionSessionStarted TranscriptionEventType = "session_started"
	// TranscriptionDisconnected 表示连接中断。
	TranscriptionDisconnected TranscriptionEventType = "disconnected"
	// TranscriptionReconnected 表示连接已经恢复。
	TranscriptionReconnected TranscriptionEventType = "reconnected"
	// TranscriptionFailed 表示转写会话失败。
	TranscriptionFailed TranscriptionEventType = "failed"
)

// RealtimeTranscriptionRequest 描述实时转写所需的业务参数。
type RealtimeTranscriptionRequest struct {
	MeetingID   string
	Format      AudioFormat
	StartSample int64
}

// TranscriptionFailure 是 adapter 向业务层传递的脱敏失败分类。
type TranscriptionFailure struct {
	Code      string
	Retryable bool
	Cause     error
}

// TranscriptionEvent 描述一次可排序的实时转写输出。
type TranscriptionEvent struct {
	Type              TranscriptionEventType
	Generation        int64
	Revision          int64
	MeetingID         string
	ResultID          string
	SessionID         string
	ProviderResultID  string
	ProviderSessionID string
	Text              string
	SpeakerID         string
	SpeakerLabel      string
	StartSample       int64
	EndSample         int64
	LastSentSample    int64
	Failure           *TranscriptionFailure
}

// RealtimeTranscriptionSession 表示一次实时转写连接。
type RealtimeTranscriptionSession interface {
	WriteFrame(ctx context.Context, frame AudioFrame) error
	LastSentSample() int64
	Events() <-chan TranscriptionEvent
	Stop(ctx context.Context) error
}

// RealtimeTranscriber 屏蔽具体 ASR 协议并创建实时转写会话。
type RealtimeTranscriber interface {
	Start(ctx context.Context, request RealtimeTranscriptionRequest) (RealtimeTranscriptionSession, error)
}
