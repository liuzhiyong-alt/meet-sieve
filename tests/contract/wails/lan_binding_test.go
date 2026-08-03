package wails_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	guestdomain "meet-sieve/internal/domain/guest"
	infraLogger "meet-sieve/internal/infra/logger"
	lanservice "meet-sieve/internal/service/lan"
	meetingservice "meet-sieve/internal/service/meeting"
	resourceservice "meet-sieve/internal/service/resource"
	guesthttp "meet-sieve/internal/transport/http/guest"
	wailstransport "meet-sieve/internal/transport/wails"
)

// fixedLANResolver 为 Wails 契约测试返回不含系统硬件地址的固定领域投影。
type fixedLANResolver struct{}

// Resolve 返回一个明确默认路由和一个手动候选。
func (fixedLANResolver) Resolve(context.Context) (guestdomain.InterfaceResolution, error) {
	return guestdomain.InterfaceResolution{Interfaces: []guestdomain.NetworkInterface{
		{ID: "lan-wifi", Name: "Wi-Fi", Address: "192.168.1.8", Up: true, DefaultRoute: true},
		{ID: "lan-usb", Name: "USB LAN", Address: "10.0.0.2", Up: true},
	}, DefaultRouteKnown: true}, nil
}

// TestLANBindingSafeProjection 验证网卡、状态和取消上传契约不泄漏代际或令牌。
func TestLANBindingSafeProjection(t *testing.T) {
	manager := lanservice.NewManager(fixedLANResolver{}, lanservice.NewRuntime(lanservice.Dependencies{}), nil, nil)
	presence := guesthttp.NewPresence()
	uploads := resourceservice.NewUploadCoordinator()
	reservation, err := uploads.ReserveAttachment(context.Background(), "meeting", "session", "request", "资料.pdf", 100)
	if err != nil {
		t.Fatalf("准备活动上传失败：%v", err)
	}
	defer reservation.Release()
	binding := wailstransport.NewLANBinding(
		func() (*lanservice.Manager, *guesthttp.Presence, *resourceservice.UploadCoordinator, *meetingservice.Service, error) {
			return manager, presence, uploads, nil, nil
		},
		func() context.Context { return context.Background() },
		wailstransport.NewBoundary(infraLogger.NewNop()),
	)

	interfaces := binding.ListLANInterfaces()
	if interfaces.Code != 200 || interfaces.Data == nil || interfaces.Data.RecommendedID != "lan-wifi" || len(interfaces.Data.Interfaces) != 2 {
		t.Fatalf("LAN 网卡 DTO 不正确：%+v", interfaces)
	}
	status := binding.GetLANStatus()
	if status.Code != 200 || status.Data == nil || status.Data.State != "disabled" {
		t.Fatalf("LAN 状态 DTO 不正确：%+v", status)
	}
	encoded, err := json.Marshal(struct {
		Interfaces any `json:"interfaces"`
		Status     any `json:"status"`
	}{Interfaces: interfaces, Status: status})
	if err != nil {
		t.Fatalf("序列化 LAN DTO 失败：%v", err)
	}
	for _, forbidden := range []string{"meeting_token", "session_token", "generation", "hardware_address"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("LAN DTO 泄漏内部字段 %q：%s", forbidden, encoded)
		}
	}
	cancelled := binding.CancelGuestUpload("request")
	if cancelled.Code != 200 || cancelled.Data == nil || !cancelled.Data.Cancelled {
		t.Fatalf("取消上传 DTO 不正确：%+v", cancelled)
	}
}
