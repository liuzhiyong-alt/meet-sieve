// Package lan 管理只在单场会议期间存在的局域网 HTTP 运行时。
package lan

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	guestdomain "meet-sieve/internal/domain/guest"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/identity"
)

const (
	// MeetingTokenBytes 是会议入口令牌的 128-bit 字节数。
	MeetingTokenBytes = 16
	shutdownTimeout   = 3 * time.Second
)

// State 是与录音、本地保存和 ASR 独立的 LAN 状态轴。
type State string

const (
	StateDisabled State = "disabled"
	StateStarting State = "starting"
	StateServing  State = "serving"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// HTTPServer 是 LANRuntime 拥有的 HTTP Server 最小生命周期边界。
type HTTPServer interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
	Close() error
}

// SessionRevoker 撤销指定会议的活动访客会话。
type SessionRevoker interface {
	RevokeMeeting(context.Context, string) error
}

// UploadCanceler 取消指定会议所有活动上传。
type UploadCanceler interface {
	CancelMeeting(string)
}

// ListenerFactory 只在选定的私有 IPv4 上创建 Listener。
type ListenerFactory func(context.Context, string) (net.Listener, error)

// ServerFactory 为每个 generation 创建独立 HTTP Server。
type ServerFactory func(http.Handler) HTTPServer

// Dependencies 是 LANRuntime 的显式资源与安全边界。
type Dependencies struct {
	IDs             identity.Generator
	Random          io.Reader
	Handler         http.Handler
	ListenerFactory ListenerFactory
	ServerFactory   ServerFactory
	Sessions        SessionRevoker
	Uploads         UploadCanceler
}

// StartRequest 指定当前会议和已由用户确认的私有网卡。
type StartRequest struct {
	MeetingID   string
	InterfaceID string
	Address     string
}

// Snapshot 是可安全给宿主 UI 展示的 LAN 状态。
type Snapshot struct {
	State       State  `json:"state"`
	MeetingID   string `json:"meeting_id,omitempty"`
	InterfaceID string `json:"interface_id,omitempty"`
	Address     string `json:"address,omitempty"`
	Generation  string `json:"generation,omitempty"`
	JoinURL     string `json:"join_url,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
}

// MeetingAccess 是 Guest session 服务验证入口令牌后所需的最小运行时事实。
type MeetingAccess struct {
	MeetingID  string
	Generation string
}

// Runtime 串行化 Start/Stop，并使令牌只存在当前进程内存。
type Runtime struct {
	ids             identity.Generator
	random          io.Reader
	handler         http.Handler
	listenerFactory ListenerFactory
	serverFactory   ServerFactory
	sessions        SessionRevoker
	uploads         UploadCanceler

	operationMu sync.Mutex
	mu          sync.RWMutex
	snapshot    Snapshot
	current     *generation
}

type generation struct {
	id           string
	meetingID    string
	interfaceID  string
	address      string
	meetingToken string
	listener     net.Listener
	server       HTTPServer
	cancel       context.CancelFunc
}

// NewRuntime 创建初始为 disabled 的单 owner LANRuntime。
func NewRuntime(dependencies Dependencies) *Runtime {
	if dependencies.Random == nil {
		dependencies.Random = rand.Reader
	}
	if dependencies.Handler == nil {
		dependencies.Handler = http.NotFoundHandler()
	}
	if dependencies.ListenerFactory == nil {
		dependencies.ListenerFactory = defaultListenerFactory
	}
	if dependencies.ServerFactory == nil {
		dependencies.ServerFactory = defaultServerFactory
	}
	return &Runtime{
		ids: dependencies.IDs, random: dependencies.Random, handler: dependencies.Handler,
		listenerFactory: dependencies.ListenerFactory, serverFactory: dependencies.ServerFactory,
		sessions: dependencies.Sessions, uploads: dependencies.Uploads,
		snapshot: Snapshot{State: StateDisabled},
	}
}

// Start 幂等启动指定会议的 LAN；选择变化时先完整停止旧 generation。
func (runtime *Runtime) Start(ctx context.Context, request StartRequest) (Snapshot, error) {
	if runtime == nil {
		return Snapshot{State: StateFailed, ErrorCode: apperr.CodeLANStartFailed.ErrorCode}, fmt.Errorf("LAN 运行时不可用")
	}
	runtime.operationMu.Lock()
	defer runtime.operationMu.Unlock()

	if snapshot, same := runtime.sameServingRequest(request); same {
		return snapshot, nil
	}
	if runtime.hasCurrentGeneration() {
		if err := runtime.stopCurrent(ctx); err != nil {
			return runtime.Snapshot(), err
		}
	}
	if request.MeetingID == "" || request.InterfaceID == "" || !guestdomain.IsPrivateBindAddress(request.Address) {
		err := apperr.Biz(apperr.CodeLANInterfaceUnavailable, apperr.WithOp("lan.runtime.validate_start"))
		runtime.setFailed(request, err.ErrorCode)
		return runtime.Snapshot(), err
	}
	return runtime.startGeneration(ctx, request)
}

// Stop 幂等停止当前 generation，上传取消和会话撤销先于 HTTP Shutdown。
func (runtime *Runtime) Stop(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	runtime.operationMu.Lock()
	defer runtime.operationMu.Unlock()
	return runtime.stopCurrent(ctx)
}

// Snapshot 返回当前 LAN 状态副本。
func (runtime *Runtime) Snapshot() Snapshot {
	if runtime == nil {
		return Snapshot{State: StateDisabled}
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.snapshot
}

// ValidateMeetingToken 使用常量时间比较校验当前 serving generation 的会议令牌。
func (runtime *Runtime) ValidateMeetingToken(meetingID string, token string) bool {
	if runtime == nil {
		return false
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if runtime.current == nil || runtime.snapshot.State != StateServing || runtime.current.meetingID != meetingID {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(runtime.current.meetingToken), []byte(token)) == 1
}

// ResolveMeetingToken 使用常量时间比较把入口令牌解析为当前会议与 generation。
func (runtime *Runtime) ResolveMeetingToken(token string) (MeetingAccess, bool) {
	if runtime == nil {
		return MeetingAccess{}, false
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if runtime.current == nil || runtime.snapshot.State != StateServing ||
		subtle.ConstantTimeCompare([]byte(runtime.current.meetingToken), []byte(token)) != 1 {
		return MeetingAccess{}, false
	}
	return MeetingAccess{MeetingID: runtime.current.meetingID, Generation: runtime.current.id}, true
}

// IsMeetingServing 判断 session 创建时的会议与 generation 是否仍在服务。
func (runtime *Runtime) IsMeetingServing(meetingID string, generationID string) bool {
	if runtime == nil {
		return false
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.current != nil && runtime.snapshot.State == StateServing &&
		runtime.current.meetingID == meetingID && runtime.current.id == generationID
}

// startGeneration 生成新令牌、绑定随机端口并启动 HTTP Serve goroutine。
func (runtime *Runtime) startGeneration(ctx context.Context, request StartRequest) (Snapshot, error) {
	runtime.setSnapshot(Snapshot{
		State: StateStarting, MeetingID: request.MeetingID, InterfaceID: request.InterfaceID, Address: request.Address,
	})
	created, err := runtime.buildGeneration(ctx, request)
	if err != nil {
		appErr := apperr.Dependency(apperr.CodeLANStartFailed, err, apperr.WithOp("lan.runtime.start"))
		runtime.setFailed(request, appErr.ErrorCode)
		return runtime.Snapshot(), appErr
	}
	runtime.mu.Lock()
	runtime.current = created
	runtime.snapshot = Snapshot{
		State: StateServing, MeetingID: request.MeetingID, InterfaceID: request.InterfaceID,
		Address: request.Address, Generation: created.id, JoinURL: buildJoinURL(request.Address, created.listener.Addr(), created.meetingToken),
	}
	runtime.mu.Unlock()
	go runtime.serve(created)
	return runtime.Snapshot(), nil
}

// buildGeneration 创建未发布的 generation，失败时不留可用令牌或 Listener。
func (runtime *Runtime) buildGeneration(ctx context.Context, request StartRequest) (*generation, error) {
	if runtime.ids == nil {
		return nil, fmt.Errorf("generation ID 生成器不可用")
	}
	generationID := runtime.ids.New()
	if generationID == "" {
		return nil, fmt.Errorf("generation ID 为空")
	}
	tokenBytes := make([]byte, MeetingTokenBytes)
	if _, err := io.ReadFull(runtime.random, tokenBytes); err != nil {
		return nil, fmt.Errorf("生成会议令牌: %w", err)
	}
	listener, err := runtime.listenerFactory(ctx, net.JoinHostPort(request.Address, "0"))
	if err != nil {
		return nil, fmt.Errorf("绑定私有网卡: %w", err)
	}
	_, cancel := context.WithCancel(context.Background())
	return &generation{
		id: generationID, meetingID: request.MeetingID, interfaceID: request.InterfaceID, address: request.Address,
		meetingToken: base64.RawURLEncoding.EncodeToString(tokenBytes), listener: listener,
		server: runtime.serverFactory(runtime.handler), cancel: cancel,
	}, nil
}

// serve 运行当前 HTTP Server，并在非预期退出时立即使令牌失效。
func (runtime *Runtime) serve(created *generation) {
	err := created.server.Serve(created.listener)
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return
	}
	runtime.mu.Lock()
	if runtime.current != created || runtime.snapshot.State == StateStopping {
		runtime.mu.Unlock()
		return
	}
	created.cancel()
	runtime.current = nil
	runtime.snapshot = Snapshot{
		State: StateFailed, MeetingID: created.meetingID, InterfaceID: created.interfaceID,
		Address: created.address, Generation: created.id, ErrorCode: apperr.CodeLANStartFailed.ErrorCode,
	}
	runtime.mu.Unlock()
	if runtime.uploads != nil {
		runtime.uploads.CancelMeeting(created.meetingID)
	}
	if runtime.sessions != nil {
		_ = runtime.sessions.RevokeMeeting(context.Background(), created.meetingID)
	}
}

// stopCurrent 在 operationMu 保护下按固定顺序回收当前 generation。
func (runtime *Runtime) stopCurrent(ctx context.Context) error {
	runtime.mu.Lock()
	current := runtime.current
	if current == nil {
		if runtime.snapshot.State != StateStopped && runtime.snapshot.State != StateDisabled {
			runtime.snapshot.JoinURL = ""
			runtime.snapshot.State = StateStopped
		}
		runtime.mu.Unlock()
		return nil
	}
	runtime.snapshot.State = StateStopping
	runtime.snapshot.JoinURL = ""
	runtime.mu.Unlock()

	current.cancel()
	if runtime.uploads != nil {
		runtime.uploads.CancelMeeting(current.meetingID)
	}
	var revokeErr error
	if runtime.sessions != nil {
		revokeErr = runtime.sessions.RevokeMeeting(ctx, current.meetingID)
	}
	shutdownErr := shutdownServer(ctx, current.server)

	runtime.mu.Lock()
	if runtime.current == current {
		runtime.current = nil
		runtime.snapshot = Snapshot{State: StateStopped, MeetingID: current.meetingID, InterfaceID: current.interfaceID, Address: current.address}
	}
	runtime.mu.Unlock()
	return errors.Join(revokeErr, shutdownErr)
}

// shutdownServer 为 graceful shutdown 提供独立 3 秒窗口，超时后强制关闭。
func shutdownServer(parent context.Context, server HTTPServer) error {
	if server == nil {
		return nil
	}
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return errors.Join(err, server.Close())
	}
	return nil
}

// sameServingRequest 判断是否为当前已服务 generation 的重复启动请求。
func (runtime *Runtime) sameServingRequest(request StartRequest) (Snapshot, bool) {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if runtime.current == nil || runtime.snapshot.State != StateServing {
		return Snapshot{}, false
	}
	same := runtime.current.meetingID == request.MeetingID && runtime.current.interfaceID == request.InterfaceID && runtime.current.address == request.Address
	return runtime.snapshot, same
}

// hasCurrentGeneration 判断是否有需要回收的 Listener/Server。
func (runtime *Runtime) hasCurrentGeneration() bool {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.current != nil
}

// setFailed 投影安全失败码，不保留会议令牌。
func (runtime *Runtime) setFailed(request StartRequest, errorCode string) {
	runtime.setSnapshot(Snapshot{
		State: StateFailed, MeetingID: request.MeetingID, InterfaceID: request.InterfaceID,
		Address: request.Address, ErrorCode: errorCode,
	})
}

// setSnapshot 在写锁下替换完整状态快照。
func (runtime *Runtime) setSnapshot(snapshot Snapshot) {
	runtime.mu.Lock()
	runtime.snapshot = snapshot
	runtime.mu.Unlock()
}

// buildJoinURL 只向宿主端状态构建含 fragment 的完整扫码入口。
func buildJoinURL(address string, listenerAddress net.Addr, token string) string {
	port := ""
	if listenerAddress != nil {
		_, parsedPort, err := net.SplitHostPort(listenerAddress.String())
		if err == nil {
			port = parsedPort
		}
	}
	if _, err := strconv.Atoi(port); err != nil {
		return ""
	}
	return "http://" + net.JoinHostPort(address, port) + "/join#k=" + token
}

// defaultListenerFactory 使用 tcp4 只绑定请求中的明确地址。
func defaultListenerFactory(ctx context.Context, address string) (net.Listener, error) {
	var config net.ListenConfig
	return config.Listen(ctx, "tcp4", address)
}

// defaultServerFactory 创建不用全局 ReadTimeout 截断大附件的 HTTP Server。
func defaultServerFactory(handler http.Handler) HTTPServer {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
