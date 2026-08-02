package volcano

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"

	"github.com/gorilla/websocket"
)

const adapterEventCapacity = 128

// AdapterConfig 描述火山实时 ASR adapter 的固定连接参数和当前凭据。
type AdapterConfig struct {
	Endpoint       string
	ResourceID     string
	Credentials    Credentials
	ConnectTimeout time.Duration
}

// Adapter 实现业务 RealtimeTranscriber port，不包含会议状态和 SQLite 逻辑。
type Adapter struct {
	config AdapterConfig
	ids    identity.Generator
	dialer *websocket.Dialer
}

// NewAdapter 创建火山实时 ASR adapter；构造阶段不建立网络连接。
func NewAdapter(config AdapterConfig, ids identity.Generator) *Adapter {
	dialer := *websocket.DefaultDialer
	return &Adapter{config: config, ids: ids, dialer: &dialer}
}

// Start 建立一个物理 WebSocket 连接并发送一次 full client request。
func (adapter *Adapter) Start(ctx context.Context, request port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	if err := adapter.validate(); err != nil {
		return nil, err
	}
	payload, err := BuildInitialPayload(request)
	if err != nil {
		return nil, apperr.Biz(apperr.CodeASRSettingsInvalid, apperr.WithOp("asr.volcano.request"))
	}
	connectID, localSessionID := adapter.ids.New(), adapter.ids.New()
	if connectID == "" || localSessionID == "" {
		return nil, fmt.Errorf("生成实时转写 session ID 失败")
	}
	headers, err := BuildHeaders(adapter.config.Credentials, adapter.config.ResourceID, connectID)
	if err != nil {
		return nil, apperr.Biz(apperr.CodeASRSettingsInvalid, apperr.WithOp("asr.volcano.headers"))
	}
	connection, response, err := adapter.dial(ctx, headers)
	if err != nil {
		return nil, mapDialError(err, response)
	}
	initialFrame, err := EncodeFullClientRequest(payload)
	if err != nil {
		_ = connection.Close()
		return nil, apperr.Dependency(apperr.CodeASRProtocolIncompatible, err, apperr.WithOp("asr.volcano.initial_encode"))
	}
	if err = connection.WriteMessage(websocket.BinaryMessage, initialFrame); err != nil {
		_ = connection.Close()
		return nil, apperr.Dependency(apperr.CodeASRStreamInterrupted, err, apperr.WithOp("asr.volcano.initial_write"))
	}
	providerSessionID := responseHeader(response, "X-Tt-Logid")
	session := newSession(connection, localSessionID, providerSessionID, request.StartSample)
	go session.readLoop()
	return session, nil
}

// validate 确保生产 adapter 不使用空 endpoint、无超时或缺失凭据。
func (adapter *Adapter) validate() error {
	if adapter == nil || adapter.ids == nil || adapter.dialer == nil || adapter.config.Endpoint == "" || adapter.config.ResourceID == "" || adapter.config.ConnectTimeout <= 0 {
		return fmt.Errorf("火山实时转写 adapter 配置无效")
	}
	if err := adapter.config.Credentials.Validate(); err != nil {
		return apperr.Biz(apperr.CodeASRSettingsInvalid, apperr.WithOp("asr.volcano.credentials"))
	}
	return nil
}

// dial 在独立连接超时内完成 WebSocket 握手。
func (adapter *Adapter) dial(ctx context.Context, headers http.Header) (*websocket.Conn, *http.Response, error) {
	connectContext, cancel := context.WithTimeout(ctx, adapter.config.ConnectTimeout)
	defer cancel()
	return adapter.dialer.DialContext(connectContext, adapter.config.Endpoint, headers)
}

// mapDialError 只依据安全状态分类错误，不读取或暴露远端响应正文。
func mapDialError(err error, response *http.Response) error {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apperr.Dependency(apperr.CodeASRConnectTimeout, err, apperr.WithOp("asr.volcano.dial"))
	}
	if response != nil {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return apperr.Dependency(apperr.CodeASRAuthFailed, err, apperr.WithOp("asr.volcano.dial"))
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			return apperr.Dependency(apperr.CodeASRServiceBusy, err, apperr.WithOp("asr.volcano.dial"))
		}
	}
	return apperr.Dependency(apperr.CodeASRStreamInterrupted, err, apperr.WithOp("asr.volcano.dial"))
}

// responseHeader 安全读取握手响应字段；空响应表示 dialer 未提供 Header。
func responseHeader(response *http.Response, key string) string {
	if response == nil {
		return ""
	}
	return response.Header.Get(key)
}

type session struct {
	connection        *websocket.Conn
	localSessionID    string
	providerSessionID string
	inputStartSample  int64

	writeMu        sync.Mutex
	pending        *port.AudioFrame
	nextSequence   int32
	acceptedEnd    int64
	lastSentSample int64
	stopping       bool

	events     chan port.TranscriptionEvent
	done       chan struct{}
	closeOnce  sync.Once
	finishOnce sync.Once
}

// newSession 创建一个单 reader/单 writer session，并预先发布连接已建立事实。
func newSession(connection *websocket.Conn, localSessionID string, providerSessionID string, inputStartSample int64) *session {
	session := &session{connection: connection, localSessionID: localSessionID, providerSessionID: providerSessionID, inputStartSample: inputStartSample, acceptedEnd: inputStartSample, events: make(chan port.TranscriptionEvent, adapterEventCapacity), done: make(chan struct{})}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionSessionStarted, SessionID: localSessionID, ProviderSessionID: providerSessionID}
	return session
}

// WriteFrame 校验连续性并保持最后一帧，便于 Stop 用负 sequence 明确提交音频尾部。
func (session *session) WriteFrame(ctx context.Context, frame port.AudioFrame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	samples := int64(len(frame.PCM) / 2)
	if frame.StartSample < 0 || samples <= 0 || len(frame.PCM)%2 != 0 {
		return fmt.Errorf("实时转写 PCM frame 无效")
	}
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if session.stopping || frame.StartSample != session.acceptedEnd {
		return fmt.Errorf("实时转写 PCM session 已停止或样本不连续")
	}
	if session.pending != nil {
		if err := session.writePendingLocked(false); err != nil {
			session.requestClose()
			return apperr.Dependency(apperr.CodeASRStreamInterrupted, err, apperr.WithOp("asr.volcano.audio_write"))
		}
	}
	copyFrame := port.AudioFrame{StartSample: frame.StartSample, PCM: append([]byte(nil), frame.PCM...)}
	session.pending = &copyFrame
	session.acceptedEnd = frame.StartSample + samples
	return nil
}

// LastSentSample 返回已实际写入 WebSocket 的最后样本边界，不把仍保留的尾帧算作已发送。
func (session *session) LastSentSample() int64 {
	if session == nil {
		return 0
	}
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	return session.lastSentSample
}

// Events 返回只读业务事件流；channel 只由 session reader 关闭。
func (session *session) Events() <-chan port.TranscriptionEvent { return session.events }

// Stop 发送唯一负 sequence 音频尾包，并等待服务端最后响应或 context 超时。
func (session *session) Stop(ctx context.Context) error {
	session.writeMu.Lock()
	if session.stopping {
		session.writeMu.Unlock()
		return session.waitDone(ctx)
	}
	session.stopping = true
	if session.pending == nil {
		session.writeMu.Unlock()
		session.requestClose()
		return session.waitDone(ctx)
	}
	err := session.writePendingLocked(true)
	session.writeMu.Unlock()
	if err != nil {
		session.requestClose()
		return apperr.Dependency(apperr.CodeASRStreamInterrupted, err, apperr.WithOp("asr.volcano.commit"))
	}
	return session.waitDone(ctx)
}

// writePendingLocked 是 session 唯一 WebSocket writer，调用方必须持有 writeMu。
func (session *session) writePendingLocked(last bool) error {
	session.nextSequence++
	frame, err := EncodeAudioOnlyRequest(session.nextSequence, last, session.pending.PCM)
	if err != nil {
		return err
	}
	if err = session.connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return err
	}
	session.lastSentSample = session.pending.StartSample + int64(len(session.pending.PCM)/2)
	session.pending = nil
	return nil
}

// waitDone 在超时或取消时主动终结连接，确保 reader goroutine 可退出。
func (session *session) waitDone(ctx context.Context) error {
	select {
	case <-session.done:
		return nil
	case <-ctx.Done():
		session.requestClose()
		return ctx.Err()
	}
}

// readLoop 是唯一 WebSocket reader，负责协议解析、事件归一化和最终关闭。
func (session *session) readLoop() {
	defer session.finish()
	for {
		messageType, data, err := session.connection.ReadMessage()
		if err != nil {
			if !session.isStopping() {
				session.publishFailure(apperr.CodeASRStreamInterrupted.ErrorCode, true, err)
			}
			return
		}
		if messageType != websocket.BinaryMessage {
			session.publishFailure(apperr.CodeASRProtocolIncompatible.ErrorCode, false, fmt.Errorf("火山返回非二进制消息"))
			return
		}
		frame, err := DecodeServerFrame(data)
		if err != nil {
			session.publishFailure(apperr.CodeASRProtocolIncompatible.ErrorCode, false, err)
			return
		}
		if session.handleServerFrame(frame) || frame.Sequence < 0 {
			return
		}
	}
}

// handleServerFrame 返回是否必须结束 session。
func (session *session) handleServerFrame(frame ServerFrame) bool {
	if frame.MessageType == messageServerACK {
		return false
	}
	if frame.MessageType == messageServerError {
		session.publishFailure(apperr.CodeASRStreamInterrupted.ErrorCode, true, fmt.Errorf("火山服务端错误码 %d", frame.ErrorCode))
		return true
	}
	session.writeMu.Lock()
	lastSentSample := session.lastSentSample
	session.writeMu.Unlock()
	events, err := ParseTranscriptionEvents(frame.Payload, session.localSessionID, session.providerSessionID, frame.Sequence, session.inputStartSample, lastSentSample)
	if err != nil {
		session.publishFailure(apperr.CodeASRProtocolIncompatible.ErrorCode, false, err)
		return true
	}
	for _, event := range events {
		event.LastSentSample = lastSentSample
		if !session.publish(event) {
			session.publishFailure(apperr.CodeASREventBackpressure.ErrorCode, true, fmt.Errorf("实时转写事件队列已满"))
			return true
		}
	}
	return false
}

// publish 对 partial 允许覆盖丢弃；final 和生命周期事件队列满时必须终止 session。
func (session *session) publish(event port.TranscriptionEvent) bool {
	select {
	case session.events <- event:
		return true
	default:
		return event.Type == port.TranscriptionPartial
	}
}

// publishFailure 只传递稳定码与内存 cause，不包含服务端正文、Header 或转写文本。
func (session *session) publishFailure(code string, retryable bool, cause error) {
	event := port.TranscriptionEvent{Type: port.TranscriptionFailed, SessionID: session.localSessionID, ProviderSessionID: session.providerSessionID, Failure: &port.TranscriptionFailure{Code: code, Retryable: retryable, Cause: cause}}
	select {
	case session.events <- event:
	default:
	}
}

// isStopping 返回显式 Stop 是否已经取得 session 终结权。
func (session *session) isStopping() bool {
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	return session.stopping
}

// requestClose 幂等关闭底层连接，使唯一 reader 从阻塞读取中退出。
func (session *session) requestClose() {
	session.closeOnce.Do(func() { _ = session.connection.Close() })
}

// finish 只由 reader 退出路径关闭事件流，避免 Stop 超时与发布并发导致 send-on-closed-channel。
func (session *session) finish() {
	session.finishOnce.Do(func() {
		session.requestClose()
		close(session.events)
		close(session.done)
	})
}
