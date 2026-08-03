package guest

import "testing"

// TestSelectPrivateInterfaces 验证 LAN 只允许明确的活动私有 IPv4 候选。
func TestSelectPrivateInterfaces(t *testing.T) {
	t.Parallel()

	interfaces := []NetworkInterface{
		{ID: "wifi", Name: "en0", Address: "192.168.1.20", Up: true, DefaultRoute: true},
		{ID: "wired", Name: "en1", Address: "10.0.0.8", Up: true},
		{ID: "public", Name: "en2", Address: "8.8.8.8", Up: true},
		{ID: "loopback", Name: "lo0", Address: "127.0.0.1", Up: true, Loopback: true},
		{ID: "link-local", Name: "en3", Address: "169.254.10.2", Up: true},
		{ID: "down", Name: "en4", Address: "172.16.0.2"},
		{ID: "vpn", Name: "utun3", Address: "10.8.0.2", Up: true},
		{ID: "docker", Name: "docker0", Address: "172.17.0.1", Up: true},
		{ID: "vm", Name: "vboxnet0", Address: "192.168.56.1", Up: true},
	}

	selection := SelectPrivateInterfaces(interfaces)
	if selection.Recommended == nil || selection.Recommended.ID != "wifi" {
		t.Fatalf("默认路由私网接口未被推荐：%#v", selection)
	}
	if len(selection.Choices) != 2 || selection.Choices[0].ID != "wifi" || selection.Choices[1].ID != "wired" {
		t.Fatalf("私网候选过滤或排序不正确：%#v", selection.Choices)
	}
}

// TestSelectPrivateInterfaces_DoesNotGuessWithoutPrivateDefault 验证无私网默认路由时不静默推荐第一项。
func TestSelectPrivateInterfaces_DoesNotGuessWithoutPrivateDefault(t *testing.T) {
	t.Parallel()

	selection := SelectPrivateInterfaces([]NetworkInterface{
		{ID: "public-default", Name: "en0", Address: "203.0.113.2", Up: true, DefaultRoute: true},
		{ID: "private", Name: "en1", Address: "192.168.2.3", Up: true},
	})
	if selection.Recommended != nil {
		t.Fatalf("无私网默认路由时不应自动推荐：%#v", selection.Recommended)
	}
	if len(selection.Choices) != 1 || selection.Reason != SelectionReasonManualRequired {
		t.Fatalf("应保留手动选择并给出原因：%#v", selection)
	}
}

// TestSelectPrivateInterfaces_ReportsUnavailable 验证没有安全候选时返回明确原因。
func TestSelectPrivateInterfaces_ReportsUnavailable(t *testing.T) {
	t.Parallel()

	selection := SelectPrivateInterfaces([]NetworkInterface{{
		ID: "public", Name: "en0", Address: "198.51.100.10", Up: true, DefaultRoute: true,
	}})
	if selection.Recommended != nil || len(selection.Choices) != 0 || selection.Reason != SelectionReasonUnavailable {
		t.Fatalf("无安全候选的结果不正确：%#v", selection)
	}
}

// TestStableInterfaceID 验证稳定网卡 ID 不直接暴露名称或 MAC。
func TestStableInterfaceID(t *testing.T) {
	t.Parallel()

	id := StableInterfaceID(7, "en0", "aa:bb:cc:dd:ee:ff")
	if id == "" || id == "en0" || id == "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("网卡 ID 不应泄漏系统标识：%q", id)
	}
	if id != StableInterfaceID(7, "en0", "aa:bb:cc:dd:ee:ff") {
		t.Fatal("相同网卡输入未生成稳定 ID")
	}
}
