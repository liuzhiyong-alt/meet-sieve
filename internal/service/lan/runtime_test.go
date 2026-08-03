package lan

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"meet-sieve/internal/infra/identity"
)

// TestRuntime_StartBindsSelectedAddressAndIsIdempotent 验证只绑定选定 IP 的随机端口且重复启动不换代。
func TestRuntime_StartBindsSelectedAddressAndIsIdempotent(t *testing.T) {
	t.Parallel()

	listener := newFakeListener("192.168.1.20:43125")
	server := newFakeServer()
	factory := &fakeListenerFactory{listener: listener}
	runtime := newTestRuntime(factory, server, nil, nil)
	request := StartRequest{MeetingID: "meeting-1", InterfaceID: "lan-interface", Address: "192.168.1.20"}

	first, err := runtime.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("启动 LAN 失败：%v", err)
	}
	second, err := runtime.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("幂等启动 LAN 失败：%v", err)
	}
	if factory.address != "192.168.1.20:0" || factory.calls != 1 {
		t.Fatalf("监听地址或调用次数不正确：address=%q calls=%d", factory.address, factory.calls)
	}
	if first.State != StateServing || first.Generation == "" || second.Generation != first.Generation {
		t.Fatalf("启动状态或幂等 generation 不正确：first=%#v second=%#v", first, second)
	}
	token := tokenFromJoinURL(t, first.JoinURL)
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != MeetingTokenBytes {
		t.Fatalf("会议令牌不是 128-bit base64url：len=%d err=%v", len(decoded), err)
	}
	if !runtime.ValidateMeetingToken("meeting-1", token) {
		t.Fatal("运行时未识别当前会议令牌")
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
}

// TestRuntime_RetryStopsOldGenerationAndRevokesSessions 验证重试前停止旧代并撤销会话。
func TestRuntime_RetryStopsOldGenerationAndRevokesSessions(t *testing.T) {
	t.Parallel()

	firstServer := newFakeServer()
	secondServer := newFakeServer()
	servers := []*fakeServer{firstServer, secondServer}
	serverIndex := 0
	revoker := &fakeRevoker{}
	uploads := &fakeUploadCanceler{}
	runtime := NewRuntime(Dependencies{
		IDs: identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
		),
		Random: strings.NewReader(strings.Repeat("a", MeetingTokenBytes) + strings.Repeat("b", MeetingTokenBytes)),
		ListenerFactory: func(_ context.Context, address string) (net.Listener, error) {
			return newFakeListener(strings.Replace(address, ":0", ":4312"+string(rune('5'+serverIndex)), 1)), nil
		},
		ServerFactory: func(_ http.Handler) HTTPServer {
			server := servers[serverIndex]
			serverIndex++
			return server
		},
		Sessions: revoker,
		Uploads:  uploads,
	})

	first, err := runtime.Start(context.Background(), StartRequest{MeetingID: "meeting-1", InterfaceID: "if-1", Address: "192.168.1.20"})
	if err != nil {
		t.Fatalf("首次启动失败：%v", err)
	}
	second, err := runtime.Start(context.Background(), StartRequest{MeetingID: "meeting-1", InterfaceID: "if-2", Address: "192.168.1.21"})
	if err != nil {
		t.Fatalf("重试启动失败：%v", err)
	}
	if first.Generation == second.Generation || first.JoinURL == second.JoinURL {
		t.Fatalf("重试没有生成新代：first=%#v second=%#v", first, second)
	}
	if firstServer.shutdownCalls != 1 || revoker.calls != 1 || uploads.calls != 1 {
		t.Fatalf("旧代回收不完整：shutdown=%d revoke=%d uploads=%d", firstServer.shutdownCalls, revoker.calls, uploads.calls)
	}
	if runtime.ValidateMeetingToken("meeting-1", tokenFromJoinURL(t, first.JoinURL)) {
		t.Fatal("旧 generation 会议令牌仍然有效")
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
}

// TestRuntime_StartFailureBecomesIndependentFailedState 验证 LAN 失败只投影到独立状态轴。
func TestRuntime_StartFailureBecomesIndependentFailedState(t *testing.T) {
	t.Parallel()

	runtime := newTestRuntime(&fakeListenerFactory{err: errors.New("bind denied")}, newFakeServer(), nil, nil)
	snapshot, err := runtime.Start(context.Background(), StartRequest{
		MeetingID: "meeting-1", InterfaceID: "if-1", Address: "192.168.1.20",
	})
	if err == nil {
		t.Fatal("绑定失败未返回错误")
	}
	if snapshot.State != StateFailed || snapshot.ErrorCode != "LAN_START_FAILED" || snapshot.JoinURL != "" {
		t.Fatalf("失败状态投影不正确：%#v", snapshot)
	}
}

// TestRuntime_StopIsIdempotent 验证停止会取消上传、撤销会话并且可重复执行。
func TestRuntime_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	server := newFakeServer()
	revoker := &fakeRevoker{}
	uploads := &fakeUploadCanceler{}
	runtime := newTestRuntime(&fakeListenerFactory{listener: newFakeListener("10.0.0.3:45000")}, server, revoker, uploads)
	if _, err := runtime.Start(context.Background(), StartRequest{MeetingID: "meeting-1", InterfaceID: "if-1", Address: "10.0.0.3"}); err != nil {
		t.Fatalf("启动失败：%v", err)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("停止失败：%v", err)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("幂等停止失败：%v", err)
	}
	if server.shutdownCalls != 1 || revoker.calls != 1 || uploads.calls != 1 {
		t.Fatalf("停止资源回收次数不正确：shutdown=%d revoke=%d uploads=%d", server.shutdownCalls, revoker.calls, uploads.calls)
	}
	if snapshot := runtime.Snapshot(); snapshot.State != StateStopped || snapshot.JoinURL != "" {
		t.Fatalf("停止后状态不正确：%#v", snapshot)
	}
}

// newTestRuntime 创建使用确定令牌和伪 Listener/Server 的运行时。
func newTestRuntime(factory *fakeListenerFactory, server *fakeServer, revoker *fakeRevoker, uploads *fakeUploadCanceler) *Runtime {
	dependencies := Dependencies{
		IDs:             identity.NewFixedGenerator("11111111-1111-4111-8111-111111111111"),
		Random:          strings.NewReader(strings.Repeat("a", MeetingTokenBytes)),
		ListenerFactory: factory.Listen,
		ServerFactory:   func(_ http.Handler) HTTPServer { return server },
	}
	if revoker != nil {
		dependencies.Sessions = revoker
	}
	if uploads != nil {
		dependencies.Uploads = uploads
	}
	return NewRuntime(dependencies)
}

// tokenFromJoinURL 从仅主持端可见的入口 URL 中读取 fragment 令牌。
func tokenFromJoinURL(t *testing.T, value string) string {
	t.Helper()
	_, token, found := strings.Cut(value, "#k=")
	if !found || token == "" {
		t.Fatalf("入口 URL 缺少 fragment 令牌：%q", value)
	}
	return token
}

type fakeListenerFactory struct {
	listener net.Listener
	err      error
	address  string
	calls    int
}

func (factory *fakeListenerFactory) Listen(_ context.Context, address string) (net.Listener, error) {
	factory.address = address
	factory.calls++
	return factory.listener, factory.err
}

type fakeListener struct {
	address net.Addr
}

func newFakeListener(address string) *fakeListener {
	return &fakeListener{address: fakeAddress(address)}
}

func (listener *fakeListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (listener *fakeListener) Close() error              { return nil }
func (listener *fakeListener) Addr() net.Addr            { return listener.address }

type fakeAddress string

func (address fakeAddress) Network() string { return "tcp" }
func (address fakeAddress) String() string  { return string(address) }

type fakeServer struct {
	mu            sync.Mutex
	shutdownCalls int
	closeCalls    int
	serveDone     chan struct{}
}

func newFakeServer() *fakeServer { return &fakeServer{serveDone: make(chan struct{})} }

func (server *fakeServer) Serve(_ net.Listener) error {
	<-server.serveDone
	return http.ErrServerClosed
}

func (server *fakeServer) Shutdown(_ context.Context) error {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.shutdownCalls++
	select {
	case <-server.serveDone:
	default:
		close(server.serveDone)
	}
	return nil
}

func (server *fakeServer) Close() error {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.closeCalls++
	select {
	case <-server.serveDone:
	default:
		close(server.serveDone)
	}
	return nil
}

type fakeRevoker struct{ calls int }

func (revoker *fakeRevoker) RevokeMeeting(_ context.Context, _ string) error {
	revoker.calls++
	return nil
}

type fakeUploadCanceler struct{ calls int }

func (canceler *fakeUploadCanceler) CancelMeeting(_ string) { canceler.calls++ }
