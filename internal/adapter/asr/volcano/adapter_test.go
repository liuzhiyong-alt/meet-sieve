package volcano

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"

	"github.com/gorilla/websocket"
)

// TestAdapterStreamsPCMAndWaitsForFinal 验证真实 WebSocket 边界上的初始化、无序号音频、空尾包和 final 事件。
func TestAdapterStreamsPCMAndWaitsForFinal(t *testing.T) {
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		responseHeaders := http.Header{"X-Tt-Logid": []string{"provider-session"}}
		connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(writer, request, responseHeaders)
		if err != nil {
			serverErrors <- err
			return
		}
		defer connection.Close()
		serverErrors <- serveAdapterFixture(connection, request.Header)
	}))
	t.Cleanup(server.Close)

	adapter := NewAdapter(AdapterConfig{Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"), ResourceID: DefaultResourceID, Credentials: Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "api-key"}, ConnectTimeout: time.Second}, identity.NewFixedGenerator("connect-id", "local-session"))
	session, err := adapter.Start(context.Background(), port.RealtimeTranscriptionRequest{MeetingID: "meeting-id", Format: port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}, StartSample: 0})
	if err != nil {
		t.Fatalf("启动火山 adapter 失败：%v", err)
	}
	if err = session.WriteFrame(context.Background(), port.AudioFrame{StartSample: 0, PCM: make([]byte, 10000)}); err != nil {
		t.Fatalf("写入第一帧失败：%v", err)
	}
	if err = session.WriteFrame(context.Background(), port.AudioFrame{StartSample: 5000, PCM: make([]byte, 44400)}); err != nil {
		t.Fatalf("写入第二帧失败：%v", err)
	}
	stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = session.Stop(stopContext); err != nil {
		t.Fatalf("停止火山 adapter 失败：%v", err)
	}
	if err = <-serverErrors; err != nil {
		t.Fatalf("fixture WebSocket 服务失败：%v", err)
	}

	var final *port.TranscriptionEvent
	for event := range session.Events() {
		if event.Type == port.TranscriptionFinal {
			copied := event
			final = &copied
		}
	}
	if final == nil || final.ProviderSessionID != "provider-session" || final.StartSample != 3200 || final.EndSample != 25600 {
		t.Fatalf("final 事件错误：%+v", final)
	}
}

// TestAdapterTreatsUnexpectedServerMessageAsRecoverable 验证单个非二进制响应进入重连，而非立即宣布协议永久不兼容。
func TestAdapterTreatsUnexpectedServerMessageAsRecoverable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_, _, _ = connection.ReadMessage()
		_ = connection.WriteMessage(websocket.TextMessage, []byte("temporary response"))
	}))
	t.Cleanup(server.Close)

	adapter := NewAdapter(AdapterConfig{Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"), ResourceID: DefaultResourceID, Credentials: Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "api-key"}, ConnectTimeout: time.Second}, identity.NewFixedGenerator("connect-id", "local-session"))
	session, err := adapter.Start(context.Background(), port.RealtimeTranscriptionRequest{MeetingID: "meeting-id", Format: port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}, StartSample: 0})
	if err != nil {
		t.Fatalf("启动火山 adapter 失败：%v", err)
	}
	for event := range session.Events() {
		if event.Type != port.TranscriptionFailed {
			continue
		}
		if event.Failure == nil || event.Failure.Code != "ASR_PROTOCOL_INCOMPATIBLE" || !event.Failure.Retryable {
			t.Fatalf("单个异常响应必须可重试：%+v", event)
		}
		return
	}
	t.Fatal("未收到可恢复的转写失败事件")
}

// serveAdapterFixture 校验新版脱敏 Header、200 ms 音频包和空尾包，再返回 final。
func serveAdapterFixture(connection *websocket.Conn, headers http.Header) error {
	if headers.Get("X-Api-Key") != "api-key" || headers.Get("X-Api-App-Key") != "" || headers.Get("X-Api-Access-Key") != "" || headers.Get("X-Api-Connect-Id") != "connect-id" {
		return fmt.Errorf("握手 Header 不符合 APP Key 契约")
	}
	_, initial, err := connection.ReadMessage()
	if err != nil {
		return err
	}
	if len(initial) < 8 || initial[0] != 0x11 || initial[1] != 0x10 {
		return fmt.Errorf("初始化帧不符合 Seed V1")
	}
	wantPacketBytes := []int{6400, 6400, 6400, 6400, 6400, 6400, 6400, 6400, 3200}
	for index, wantBytes := range wantPacketBytes {
		_, audioFrame, readErr := connection.ReadMessage()
		if readErr != nil {
			return readErr
		}
		pcm, decodeErr := decodeClientAudioFrame(audioFrame, false)
		if decodeErr != nil || len(pcm) != wantBytes {
			return fmt.Errorf("第 %d 个 audio 包错误：bytes=%d err=%v", index+1, len(pcm), decodeErr)
		}
	}
	_, lastAudio, err := connection.ReadMessage()
	if err != nil {
		return err
	}
	if _, err = decodeClientAudioFrame(lastAudio, true); err != nil {
		return fmt.Errorf("最后 audio 空尾包错误：%w", err)
	}
	return writeFinalFixture(connection)
}

// decodeClientAudioFrame 仅供 WebSocket 契约测试校验客户端发包边界。
func decodeClientAudioFrame(data []byte, last bool) ([]byte, error) {
	header, body, err := decodeHeader(data)
	if err != nil {
		return nil, err
	}
	wantFlag := byte(flagNoSequence)
	if last {
		wantFlag = flagLastPacket
	}
	if header.messageType != messageAudioOnlyRequest || header.flags != wantFlag || header.compression != compressionGzip {
		return nil, fmt.Errorf("audio Header 不符合契约")
	}
	payload, err := decodeSizedPayload(body)
	if err != nil {
		return nil, err
	}
	pcm, err := decompressPayload(payload, header.compression)
	if err != nil {
		return nil, err
	}
	if last && len(pcm) != 0 {
		return nil, fmt.Errorf("最后 audio 包必须为空")
	}
	return pcm, nil
}

// writeFinalFixture 把脱敏响应编码为优化流式端点的无 sequence full response。
func writeFinalFixture(connection *websocket.Conn) error {
	path := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "asr", "volcano", "legacy_final_response.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	compressed, err := gzipPayload(payload)
	if err != nil {
		return err
	}
	frame := encodePayloadFrame(messageFullServerResponse, flagNoSequence, serializationJSON, compressionGzip, nil, compressed)
	return connection.WriteMessage(websocket.BinaryMessage, frame)
}
