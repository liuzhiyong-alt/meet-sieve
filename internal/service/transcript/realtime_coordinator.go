package transcript

import (
	"context"
	"fmt"
	"sync"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	transcriptrepository "meet-sieve/internal/repository/transcript"
)

// ReconnectWaitFunc 等待一次重连退避，可由测试替换为可控同步点。
type ReconnectWaitFunc func(context.Context, time.Duration) error

// RealtimeStatePublisher 发布独立 ASR 状态轴，不承载凭据或错误 cause。
type RealtimeStatePublisher func(meetingID string, state string, errorCode string)

// RealtimeCoordinatorDependencies 描述实时 session、事件持久化和退避依赖。
type RealtimeCoordinatorDependencies struct {
	// Repository 读写 session 与会议 ASR 状态。
	Repository *transcriptrepository.Repository
	// Transactions 提供单 writer 短事务。
	Transactions *database.TransactionManager
	// Events 持久化 final 与 gap 统一事件。
	Events *EventService
	// Transcriber 根据凭据创建厂商 adapter。
	Transcriber TranscriberFactory
	// IDs 创建本地物理 session ID。
	IDs identity.Generator
	// Clock 提供审计时间。
	Clock clock.Clock
	// Backoff 是五次自动重连退避。
	Backoff []time.Duration
	// Wait 执行可取消退避。
	Wait ReconnectWaitFunc
	// PublishPartial 发布内存 partial。
	PublishPartial PartialPublisher
	// PublishState 发布安全状态。
	PublishState RealtimeStatePublisher
	// FinalPersistTimeout 限制单条 final SQLite 事务时间。
	FinalPersistTimeout time.Duration
	// FinalQueueCapacity 是允许等待持久化的 final 数量。
	FinalQueueCapacity int
}

type realtimeFailure struct {
	generation int64
	code       string
	retryable  bool
	reason     transcriptdomain.GapReason
	resumeAt   int64
	cause      error
}

type physicalSession struct {
	id         string
	generation int64
	inputStart int64
	remote     port.RealtimeTranscriptionSession
	senderDone chan struct{}
}

// RealtimeCoordinator 编排一个会议的 PCM 旁路、物理连接、重连和精确 gap。
type RealtimeCoordinator struct {
	dependencies RealtimeCoordinatorDependencies
	queue        *PCMQueue
	partials     *PartialProjector
	finals       *FinalProcessor

	mu             sync.Mutex
	ctx            context.Context
	cancel         context.CancelFunc
	meetingID      string
	credentials    transcriptdomain.Credentials
	current        *physicalSession
	generation     int64
	reconnectCount int
	lastSent       int64
	lastFinal      int64
	gapStart       *int64
	gapReason      transcriptdomain.GapReason
	gapSessionID   *string
	started        bool
	stopping       bool
	unavailable    bool

	failures chan realtimeFailure
	done     chan struct{}
}

// NewRealtimeCoordinator 创建会议级实时转写器；显式 Start 前不建立连接或 goroutine。
func NewRealtimeCoordinator(dependencies RealtimeCoordinatorDependencies) *RealtimeCoordinator {
	if dependencies.Wait == nil {
		dependencies.Wait = waitReconnect
	}
	return &RealtimeCoordinator{dependencies: dependencies, queue: NewPCMQueue(), failures: make(chan realtimeFailure, 8), done: make(chan struct{})}
}

// Start 建立首个物理连接；外部连接失败进入后台重连，不回滚已经开始的本地录音。
func (coordinator *RealtimeCoordinator) Start(ctx context.Context, meetingID string, startSample int64, credentials transcriptdomain.Credentials) error {
	if err := coordinator.validateStart(ctx, meetingID, startSample, credentials); err != nil {
		return err
	}
	coordinator.mu.Lock()
	coordinator.ctx, coordinator.cancel = context.WithCancel(ctx)
	coordinator.meetingID, coordinator.credentials = meetingID, credentials
	coordinator.lastSent, coordinator.lastFinal, coordinator.started = startSample, startSample, true
	coordinator.partials = NewPartialProjector(coordinator.dependencies.Clock.Now, coordinator.dependencies.PublishPartial)
	coordinator.finals = NewFinalProcessor(FinalProcessorDependencies{
		Capacity: coordinator.dependencies.FinalQueueCapacity, PersistTimeout: coordinator.dependencies.FinalPersistTimeout,
		Persist: coordinator.persistFinal,
		OnFailure: func(err error) {
			coordinator.reportFailure(realtimeFailure{code: apperr.CodeASREventPersistFailed.ErrorCode, reason: transcriptdomain.GapBackpressure, cause: err})
		},
	})
	runContext := coordinator.ctx
	coordinator.mu.Unlock()
	if err := coordinator.finals.Start(runContext); err != nil {
		return err
	}
	go coordinator.supervise()
	go coordinator.connectInitial(startSample)
	return nil
}

// connectInitial 在录音旁路已可接收后异步连接，网络慢或失败不能阻塞本地录音开始。
func (coordinator *RealtimeCoordinator) connectInitial(startSample int64) {
	if err := coordinator.connect(startSample, 0); err != nil {
		coordinator.openGap(startSample, transcriptdomain.GapConnectFailed, nil)
		coordinator.reportFailure(realtimeFailure{code: errorCodeOf(err), retryable: true, reason: transcriptdomain.GapConnectFailed, resumeAt: startSample, cause: err})
	}
}

// TryAcceptFrame 非阻塞接收已安全写入 WAV 的 PCM；背压只终止 ASR。
func (coordinator *RealtimeCoordinator) TryAcceptFrame(frame port.AudioFrame) bool {
	coordinator.mu.Lock()
	active := coordinator.started && !coordinator.stopping && !coordinator.unavailable
	generation := coordinator.generation
	coordinator.mu.Unlock()
	if !active || !coordinator.queue.TryAcceptFrame(frame) {
		if active {
			coordinator.reportFailure(realtimeFailure{generation: generation, code: apperr.CodeASREventBackpressure.ErrorCode, reason: transcriptdomain.GapBackpressure, cause: fmt.Errorf("ASR PCM 队列已满")})
		}
		return false
	}
	return true
}

// Stop 停止接收、提交音频尾包、排空 final，并在超时或不可用时形成精确尾部 gap。
func (coordinator *RealtimeCoordinator) Stop(ctx context.Context, recordingEndSample int64) error {
	if coordinator == nil || ctx == nil || recordingEndSample < 0 {
		return fmt.Errorf("停止实时转写参数无效")
	}
	coordinator.mu.Lock()
	if !coordinator.started {
		coordinator.mu.Unlock()
		return nil
	}
	if coordinator.stopping {
		done := coordinator.done
		coordinator.mu.Unlock()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	coordinator.stopping = true
	current := coordinator.current
	coordinator.queue.Close()
	coordinator.mu.Unlock()

	var tailFailure bool
	if current != nil {
		select {
		case <-current.senderDone:
		case <-ctx.Done():
			tailFailure = true
		}
		if !tailFailure {
			if err := current.remote.Stop(ctx); err != nil {
				tailFailure = true
			} else {
				coordinator.checkpointSent(current, current.remote.LastSentSample())
			}
		}
	} else {
		tailFailure = true
	}
	if err := coordinator.finals.CloseAndWait(ctx); err != nil {
		tailFailure = true
	}
	if tailFailure {
		coordinator.openTailGap(recordingEndSample)
	}
	if err := coordinator.persistOpenGap(ctx, recordingEndSample); err != nil {
		return err
	}
	coordinator.finishCurrent("stopped", "stopped", nil)
	coordinator.mu.Lock()
	if coordinator.cancel != nil {
		coordinator.cancel()
	}
	coordinator.started = false
	select {
	case <-coordinator.done:
	default:
		close(coordinator.done)
	}
	coordinator.mu.Unlock()
	return nil
}

// Retry 在 unavailable 状态下由用户显式重置五次退避链并创建新 session。
func (coordinator *RealtimeCoordinator) Retry() error {
	if coordinator == nil {
		return fmt.Errorf("实时转写 coordinator 不可用")
	}
	coordinator.mu.Lock()
	if !coordinator.started || coordinator.stopping || !coordinator.unavailable {
		coordinator.mu.Unlock()
		return fmt.Errorf("当前实时转写状态不允许手动重试")
	}
	coordinator.unavailable = false
	coordinator.reconnectCount = 0
	start := coordinator.lastSent
	coordinator.mu.Unlock()
	coordinator.publishState("connecting", "")
	if err := coordinator.connect(start, 0); err != nil {
		coordinator.reportFailure(realtimeFailure{code: errorCodeOf(err), retryable: true, reason: transcriptdomain.GapConnectFailed, resumeAt: start, cause: err})
		return err
	}
	return nil
}

// validateStart 检查冻结依赖和输入，不在失败时做部分初始化。
func (coordinator *RealtimeCoordinator) validateStart(ctx context.Context, meetingID string, startSample int64, credentials transcriptdomain.Credentials) error {
	if coordinator == nil || ctx == nil || meetingID == "" || startSample < 0 || coordinator.dependencies.Repository == nil || coordinator.dependencies.Transactions == nil || coordinator.dependencies.Events == nil || coordinator.dependencies.Transcriber == nil || coordinator.dependencies.IDs == nil || coordinator.dependencies.Clock == nil || coordinator.dependencies.FinalPersistTimeout <= 0 || coordinator.dependencies.FinalQueueCapacity != 128 || len(coordinator.dependencies.Backoff) != 5 {
		return fmt.Errorf("实时转写 coordinator 依赖或输入无效")
	}
	if err := credentials.Validate(); err != nil {
		return err
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.started {
		return fmt.Errorf("实时转写 coordinator 已启动")
	}
	return nil
}

// connect 创建一条本地 session 事实，再建立唯一厂商连接并启动 reader/writer。
