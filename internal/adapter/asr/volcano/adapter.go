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

const maxTimestampToleranceSamples int64 = 16000 / 5

const audioPacketSamples int64 = 16000 / 5

const audioPacketBytes = int(audioPacketSamples * 2)

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
	if err = writeInitialFrame(connection, initialFrame, adapter.config.ConnectTimeout); err != nil {
		_ = connection.Close()
		return nil, apperr.Dependency(apperr.CodeASRStreamInterrupted, err, apperr.WithOp("asr.volcano.initial_write"))
	}
	providerSessionID := responseHeader(response, "X-Tt-Logid")
	session := newSession(connection, localSessionID, providerSessionID, request.StartSample, adapter.config.ConnectTimeout)
	go session.readLoop()
	return session, nil
}

// writeInitialFrame 限制握手后的首次协议写入，避免 TCP 建连成功后永久卡住。
func writeInitialFrame(connection *websocket.Conn, frame []byte, timeout time.Duration) error {
	if connection == nil || len(frame) == 0 || timeout <= 0 {
		return fmt.Errorf("实时转写初始化写入参数无效")
	}
	if err := connection.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("设置实时转写初始化写入超时失败：%w", err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return err
	}
	if err := connection.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("清理实时转写初始化写入超时失败：%w", err)
	}
	return nil
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
	writeTimeout      time.Duration

	writeMu          sync.Mutex
	acceptedEnd      int64
	lastSentSample   int64
	lastFrameSamples int64
	pendingPCM       []byte
	responseSeq      int32
	stopping         bool

	events     chan port.TranscriptionEvent
	done       chan struct{}
	closeOnce  sync.Once
	finishOnce sync.Once
}

// newSession 创建一个单 reader/单 writer session，并预先发布连接已建立事实。
func newSession(connection *websocket.Conn, localSessionID string, providerSessionID string, inputStartSample int64, writeTimeout time.Duration) *session {
	session := &session{connection: connection, localSessionID: localSessionID, providerSessionID: providerSessionID, inputStartSample: inputStartSample, writeTimeout: writeTimeout, acceptedEnd: inputStartSample, lastSentSample: inputStartSample, events: make(chan port.TranscriptionEvent, adapterEventCapacity), done: make(chan struct{})}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionSessionStarted, SessionID: localSessionID, ProviderSessionID: providerSessionID}
	return session
}

// WriteFrame 校验连续性，并把操作系统回调重新分包为火山建议的 200 ms PCM。
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
	session.acceptedEnd = frame.StartSample + samples
	session.pendingPCM = append(session.pendingPCM, frame.PCM...)
	for len(session.pendingPCM) >= audioPacketBytes {
		if err := session.writePCMChunkLocked(ctx, session.pendingPCM[:audioPacketBytes]); err != nil {
			return err
		}
		session.pendingPCM = session.pendingPCM[audioPacketBytes:]
	}
	if len(session.pendingPCM) == 0 {
		session.pendingPCM = nil
	}
	return nil
}

// writePCMChunkLocked 发送一个普通音频包，调用方必须持有 writeMu。
func (session *session) writePCMChunkLocked(ctx context.Context, pcm []byte) error {
	wireFrame, err := EncodeAudioOnlyRequest(false, pcm)
	if err != nil {
		return apperr.Dependency(apperr.CodeASRProtocolIncompatible, err, apperr.WithOp("asr.volcano.audio_encode"))
	}
	if err = session.writeMessageLocked(ctx, wireFrame); err != nil {
		session.requestClose()
		return apperr.Dependency(apperr.CodeASRStreamInterrupted, err, apperr.WithOp("asr.volcano.audio_write"))
	}
	samples := int64(len(pcm) / 2)
	session.lastSentSample += samples
	session.lastFrameSamples = samples
	return nil
}

// writeMessageLocked 为会中每次 PCM 写入设置 deadline，调用方必须持有 writeMu。
func (session *session) writeMessageLocked(ctx context.Context, frame []byte) error {
	if ctx == nil || session.writeTimeout <= 0 {
		return fmt.Errorf("实时转写写入超时参数无效")
	}
	deadline := time.Now().Add(session.writeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := session.connection.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("设置实时转写写入超时失败：%w", err)
	}
	if err := session.connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return err
	}
	if err := session.connection.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("清理实时转写写入超时失败：%w", err)
	}
	return nil
}

// LastSentSample 返回已实际写入 WebSocket 的最后样本边界。
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

// Stop 发送优化流式协议的空尾包，并等待服务端最后响应或 context 超时。
func (session *session) Stop(ctx context.Context) error {
	session.writeMu.Lock()
	if session.stopping {
		session.writeMu.Unlock()
		return session.waitDone(ctx)
	}
	session.stopping = true
	var err error
	// 停止时先提交不足 200 ms 的真实尾部音频，再发送空负包。
	if len(session.pendingPCM) > 0 {
		err = session.writePCMChunkLocked(ctx, session.pendingPCM)
		session.pendingPCM = nil
	}
	var frame []byte
	if err == nil {
		frame, err = EncodeAudioOnlyRequest(true, nil)
	}
	if err == nil {
		err = session.writeMessageLocked(ctx, frame)
	}
	session.writeMu.Unlock()
	if err != nil {
		session.requestClose()
		return apperr.Dependency(apperr.CodeASRStreamInterrupted, err, apperr.WithOp("asr.volcano.commit"))
	}
	return session.waitDone(ctx)
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
			session.publishFailure(apperr.CodeASRProtocolIncompatible.ErrorCode, true, fmt.Errorf("火山返回非二进制消息"))
			return
		}
		frame, err := DecodeServerFrame(data)
		if err != nil {
			session.publishFailure(apperr.CodeASRProtocolIncompatible.ErrorCode, true, err)
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
	timestampTolerance := timestampToleranceSamples(session.lastFrameSamples)
	session.writeMu.Unlock()
	responseSequence := frame.Sequence
	if responseSequence == 0 {
		session.responseSeq++
		responseSequence = session.responseSeq
	}
	events, err := parseTranscriptionEvents(frame.Payload, session.localSessionID, session.providerSessionID, responseSequence, session.inputStartSample, lastSentSample, timestampTolerance)
	if err != nil {
		session.publishFailure(apperr.CodeASRProtocolIncompatible.ErrorCode, true, err)
		return true
	}
	for _, event := range events {
		event.LastSentSample = lastSentSample
		if !session.publish(event) {
			session.publishFailure(apperr.CodeASREventBackpressure.ErrorCode, false, fmt.Errorf("实时转写事件队列已满"))
			return true
		}
	}
	return false
}

// timestampToleranceSamples 只容忍一个实际发送 PCM 帧或毫秒取整带来的边界抖动。
func timestampToleranceSamples(lastFrameSamples int64) int64 {
	if lastFrameSamples < 16 {
		return 16
	}
	if lastFrameSamples > maxTimestampToleranceSamples {
		return maxTimestampToleranceSamples
	}
	return lastFrameSamples
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
