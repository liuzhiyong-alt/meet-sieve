package network

import "testing"

// TestParseDarwinDefaultInterface 验证 macOS route 输出只提取默认路由网卡名。
func TestParseDarwinDefaultInterface(t *testing.T) {
	t.Parallel()

	output := []byte("   route to: default\ndestination: default\n  interface: en0\n      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING>\n")
	name, ok := parseDarwinDefaultInterface(output)
	if !ok || name != "en0" {
		t.Fatalf("解析 macOS 默认路由失败：name=%q ok=%v", name, ok)
	}
	if _, ok := parseDarwinDefaultInterface([]byte("interface:\n")); ok {
		t.Fatal("空网卡名不应被接受")
	}
}

// TestParseWindowsDefaultInterfaceAddress 验证 Windows route print 输出按最小 metric 选择默认路由地址。
func TestParseWindowsDefaultInterfaceAddress(t *testing.T) {
	t.Parallel()

	output := []byte(`IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0      10.20.30.1     10.20.30.44     35
          0.0.0.0          0.0.0.0    192.168.1.1    192.168.1.22     15
`)
	address, ok := parseWindowsDefaultInterfaceAddress(output)
	if !ok || address != "192.168.1.22" {
		t.Fatalf("解析 Windows 默认路由失败：address=%q ok=%v", address, ok)
	}
}
