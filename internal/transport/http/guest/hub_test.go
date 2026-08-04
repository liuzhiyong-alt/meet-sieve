package guest

import (
	"net/http/httptest"
	"testing"
)

// TestHubSlowSubscriberKeepsLatestNotification 验证慢客户端只占一个缓冲并收到最新 seq。
func TestHubSlowSubscriberKeepsLatestNotification(t *testing.T) {
	hub := NewHub()
	notifications, unsubscribe := hub.Subscribe("meeting-1")
	defer unsubscribe()
	hub.Publish("meeting-1", 2, "first")
	hub.Publish("meeting-1", 5, "latest")

	notification := <-notifications
	if notification.LatestSeq != 5 || notification.Reason != "latest" || notification.Type != "timeline.changed" {
		t.Fatalf("慢订阅者未收到最新通知：%#v", notification)
	}
}

// TestWebSocketOriginAllowedRequiresExactOrigin 验证 WS GET 也不能绕过写请求同源边界。
func TestWebSocketOriginAllowedRequiresExactOrigin(t *testing.T) {
	request := httptest.NewRequest("GET", "http://192.168.1.2:52203/api/v1/guest/ws", nil)
	request.Header.Set("Origin", "http://192.168.1.2:52203")
	if !websocketOriginAllowed(request, func() string { return "http://192.168.1.2:52203" }) {
		t.Fatal("完全同源的 WebSocket 握手应被允许")
	}
	request.Header.Set("Origin", "http://evil.example")
	if websocketOriginAllowed(request, func() string { return "http://192.168.1.2:52203" }) {
		t.Fatal("跨源 WebSocket 握手不应被允许")
	}
}
