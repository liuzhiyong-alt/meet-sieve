package transcript

import (
	"context"
	"fmt"
	"sync"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	transcriptrepository "meet-sieve/internal/repository/transcript"

	"gorm.io/gorm"
)

const (
	// MeetingASRModeRealtime 为会议启用火山实时转写。
	MeetingASRModeRealtime = "realtime"
	// MeetingASRModeRecordOnly 只保存本地录音并登记完整转写 gap。
	MeetingASRModeRecordOnly = "record_only"
)

// MeetingRuntimeDependencies 描述会议生命周期所需的转写服务依赖。
type MeetingRuntimeDependencies struct {
	// Settings 提供当前模式凭据。
	Settings *SettingsService
	// Repository 读写会议 ASR 状态与 session。
	Repository *transcriptrepository.Repository
	// Transactions 提供短事务。
	Transactions *database.TransactionManager
	// Events 持久化 final 和 gap。
	Events *EventService
	// Transcriber 创建厂商 adapter。
	Transcriber TranscriberFactory
	// IDs 创建物理 session ID。
	IDs identity.Generator
	// Clock 提供审计时间。
	Clock clock.Clock
	// Backoff 是五次自动重连退避。
	Backoff []time.Duration
	// FinalPersistTimeout 是单条 final 持久化上限。
	FinalPersistTimeout time.Duration
	// FinalQueueCapacity 是 final 有界队列容量。
	FinalQueueCapacity int
	// PublishPartial 发布会中临时文本。
	PublishPartial PartialPublisher
	// PublishState 发布实时转写状态。
	PublishState RealtimeStatePublisher
	// RawRecord 从 SQLite 重建会议原始记录。
	RawRecord *RawRecordProjector
	// WorkspaceRoot 是已验证的会议工作目录。
	WorkspaceRoot string
}

// MeetingRuntime 为会议录音服务提供单活动转写运行时。
type MeetingRuntime struct {
	dependencies MeetingRuntimeDependencies
	mu           sync.Mutex
	meetingID    string
	mode         string
	coordinator  *RealtimeCoordinator
	lastStopped  string
}

// NewMeetingRuntime 创建会议级转写运行时；构造阶段不读取凭据或建立网络。
func NewMeetingRuntime(dependencies MeetingRuntimeDependencies) *MeetingRuntime {
	return &MeetingRuntime{dependencies: dependencies}
}

// Start 为当前会议启动 realtime，或进入明确的 record_only 状态。
func (runtime *MeetingRuntime) Start(ctx context.Context, meetingID string, mode string) error {
	if err := runtime.validate(ctx, meetingID, mode); err != nil {
		return err
	}
	runtime.mu.Lock()
	if runtime.meetingID != "" {
		runtime.mu.Unlock()
		return fmt.Errorf("已有活动会议转写运行时")
	}
	runtime.meetingID, runtime.mode, runtime.lastStopped = meetingID, mode, ""
	runtime.mu.Unlock()
	if mode == MeetingASRModeRecordOnly {
		return runtime.updateMeetingState(ctx, "unavailable")
	}
	credentials, err := runtime.dependencies.Settings.CurrentCredentials(ctx)
	if err != nil {
		runtime.clearActive()
		return err
	}
	coordinator := NewRealtimeCoordinator(RealtimeCoordinatorDependencies{
		Repository: runtime.dependencies.Repository, Transactions: runtime.dependencies.Transactions,
		Events: runtime.dependencies.Events, Transcriber: runtime.dependencies.Transcriber,
		IDs: runtime.dependencies.IDs, Clock: runtime.dependencies.Clock, Backoff: runtime.dependencies.Backoff,
		FinalPersistTimeout: runtime.dependencies.FinalPersistTimeout, FinalQueueCapacity: runtime.dependencies.FinalQueueCapacity,
		PublishPartial: runtime.dependencies.PublishPartial, PublishState: runtime.dependencies.PublishState,
	})
	runtime.mu.Lock()
	runtime.coordinator = coordinator
	runtime.mu.Unlock()
	if err = coordinator.Start(ctx, meetingID, 0, credentials); err != nil {
		runtime.clearActive()
		return err
	}
	return nil
}

// TryAcceptFrame 把已持久化 PCM 非阻塞交给当前 realtime；record_only 明确忽略。
func (runtime *MeetingRuntime) TryAcceptFrame(frame port.AudioFrame) bool {
	runtime.mu.Lock()
	coordinator, mode := runtime.coordinator, runtime.mode
	runtime.mu.Unlock()
	if mode == MeetingASRModeRecordOnly {
		return true
	}
	return coordinator != nil && coordinator.TryAcceptFrame(frame)
}

// Stop 按会议模式停止实时 session，或登记整段 record_only gap；成功后保持幂等。
func (runtime *MeetingRuntime) Stop(ctx context.Context, meetingID string, recordingEndSample int64) error {
	if runtime == nil || ctx == nil || meetingID == "" || recordingEndSample < 0 {
		return fmt.Errorf("停止会议转写参数无效")
	}
	runtime.mu.Lock()
	if runtime.lastStopped == meetingID {
		runtime.mu.Unlock()
		return nil
	}
	if runtime.meetingID != meetingID {
		runtime.mu.Unlock()
		return fmt.Errorf("会议转写运行时不匹配")
	}
	mode, coordinator := runtime.mode, runtime.coordinator
	runtime.mu.Unlock()
	var err error
	if mode == MeetingASRModeRecordOnly {
		err = runtime.persistRecordOnlyGap(ctx, meetingID, recordingEndSample)
	} else if coordinator != nil {
		err = coordinator.Stop(ctx, recordingEndSample)
	}
	if err != nil {
		return err
	}
	if err = runtime.rebuildRawRecord(ctx, meetingID); err != nil {
		return err
	}
	runtime.mu.Lock()
	runtime.lastStopped = meetingID
	runtime.meetingID, runtime.mode, runtime.coordinator = "", "", nil
	runtime.mu.Unlock()
	return nil
}

// rebuildRawRecord 在 final/gap 全部稳定后从 SQLite 重建确定性 Markdown。
func (runtime *MeetingRuntime) rebuildRawRecord(ctx context.Context, meetingID string) error {
	if runtime.dependencies.RawRecord == nil || runtime.dependencies.WorkspaceRoot == "" {
		return fmt.Errorf("原始记录投影运行时未初始化")
	}
	return runtime.dependencies.RawRecord.Flush(ctx, meetingID)
}

// Retry 重置当前会议 realtime 的自动退避链。
func (runtime *MeetingRuntime) Retry(meetingID string) error {
	runtime.mu.Lock()
	coordinator, currentID := runtime.coordinator, runtime.meetingID
	runtime.mu.Unlock()
	if currentID != meetingID || coordinator == nil {
		return fmt.Errorf("当前会议没有可重试的实时转写")
	}
	return coordinator.Retry()
}

// RawRecordState 返回当前会议原始记录投影状态，不暴露工作目录或文件错误正文。
func (runtime *MeetingRuntime) RawRecordState(meetingID string) RawRecordProjectionState {
	if runtime == nil || runtime.dependencies.RawRecord == nil {
		return RawRecordProjectionState{State: "idle"}
	}
	return runtime.dependencies.RawRecord.State(meetingID)
}

// validate 检查单活动运行时的冻结依赖与模式。
func (runtime *MeetingRuntime) validate(ctx context.Context, meetingID string, mode string) error {
	if runtime == nil || ctx == nil || meetingID == "" || runtime.dependencies.Settings == nil || runtime.dependencies.Repository == nil || runtime.dependencies.Transactions == nil || runtime.dependencies.Events == nil || runtime.dependencies.Transcriber == nil || runtime.dependencies.IDs == nil || runtime.dependencies.Clock == nil || runtime.dependencies.RawRecord == nil || runtime.dependencies.WorkspaceRoot == "" || len(runtime.dependencies.Backoff) != 5 || runtime.dependencies.FinalPersistTimeout <= 0 || runtime.dependencies.FinalQueueCapacity != 128 {
		return fmt.Errorf("会议转写运行时依赖无效")
	}
	if mode != MeetingASRModeRealtime && mode != MeetingASRModeRecordOnly {
		return fmt.Errorf("会议 ASR 模式无效")
	}
	return nil
}

// persistRecordOnlyGap 写入整段录音唯一 gap；零样本会议无需生成空区间。
func (runtime *MeetingRuntime) persistRecordOnlyGap(ctx context.Context, meetingID string, endSample int64) error {
	if endSample == 0 {
		return nil
	}
	rangeValue, err := transcriptdomain.NewSampleRange(0, endSample)
	if err != nil {
		return err
	}
	_, err = runtime.dependencies.Events.PersistGap(ctx, GapInput{MeetingID: meetingID, Range: rangeValue, Reason: transcriptdomain.GapRecordOnly})
	return err
}

// updateMeetingState 更新 record_only 的独立 ASR 状态轴。
func (runtime *MeetingRuntime) updateMeetingState(ctx context.Context, state string) error {
	now := runtime.dependencies.Clock.Now().UnixMilli()
	return runtime.dependencies.Transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return runtime.dependencies.Repository.UpdateMeetingASRState(ctx, tx, runtime.meetingID, state, now)
	})
}

// clearActive 清理启动失败的内存运行时。
func (runtime *MeetingRuntime) clearActive() {
	runtime.mu.Lock()
	runtime.meetingID, runtime.mode, runtime.coordinator = "", "", nil
	runtime.mu.Unlock()
}
