package volcano

import (
	"context"
	"encoding/binary"
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

// TestAdapterStreamsPCMAndWaitsForFinal 验证真实 WebSocket 边界上的初始化、首尾 sequence 和 final 事件。
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

	adapter := NewAdapter(AdapterConfig{Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"), ResourceID: DefaultResourceID, Credentials: Credentials{Mode: transcriptdomain.AuthModeLegacy, AppID: "app-id", AccessToken: "access-token"}, ConnectTimeout: time.Second}, identity.NewFixedGenerator("connect-id", "local-session"))
	session, err := adapter.Start(context.Background(), port.RealtimeTranscriptionRequest{MeetingID: "meeting-id", Format: port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}, StartSample: 0})
	if err != nil {
		t.Fatalf("启动火山 adapter 失败：%v", err)
	}
	if err = session.WriteFrame(context.Background(), port.AudioFrame{StartSample: 0, PCM: make([]byte, 32000)}); err != nil {
		t.Fatalf("写入第一帧失败：%v", err)
	}
	if err = session.WriteFrame(context.Background(), port.AudioFrame{StartSample: 16000, PCM: make([]byte, 32000)}); err != nil {
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

// serveAdapterFixture 校验脱敏 Header 和三类客户端帧，再返回一条负 sequence final。
func serveAdapterFixture(connection *websocket.Conn, headers http.Header) error {
	if headers.Get("X-Api-App-Key") != "app-id" || headers.Get("X-Api-Access-Key") != "access-token" || headers.Get("X-Api-Connect-Id") != "connect-id" {
		return fmt.Errorf("握手 Header 不符合 legacy 契约")
	}
	_, initial, err := connection.ReadMessage()
	if err != nil {
		return err
	}
	if len(initial) < 8 || initial[0] != 0x11 || initial[1] != 0x10 {
		return fmt.Errorf("初始化帧不符合 Seed V1")
	}
	_, firstAudio, err := connection.ReadMessage()
	if err != nil {
		return err
	}
	if len(firstAudio) < 12 || firstAudio[1] != 0x21 || int32(binary.BigEndian.Uint32(firstAudio[4:8])) != 1 {
		return fmt.Errorf("首个 audio sequence 错误")
	}
	_, lastAudio, err := connection.ReadMessage()
	if err != nil {
		return err
	}
	if len(lastAudio) < 12 || lastAudio[1] != 0x23 || int32(binary.BigEndian.Uint32(lastAudio[4:8])) != -2 {
		return fmt.Errorf("最后 audio sequence 错误")
	}
	return writeFinalFixture(connection)
}

// writeFinalFixture 把脱敏官方响应形状编码为最后一条 full server response。
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
	sequence := make([]byte, 4)
	negativeSequence := int32(-9)
	binary.BigEndian.PutUint32(sequence, uint32(negativeSequence))
	frame := encodePayloadFrame(messageFullServerResponse, flagNegativeSequence, serializationJSON, compressionGzip, sequence, compressed)
	return connection.WriteMessage(websocket.BinaryMessage, frame)
}
