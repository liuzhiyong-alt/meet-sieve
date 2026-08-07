package assets

import (
	"net/http"
	"testing"
)

// TestNewLocalProxyHTTPClient 验证配置端口时固定走本机代理，零值时不继承终端代理。
func TestNewLocalProxyHTTPClient(t *testing.T) {
	t.Parallel()

	withProxy, err := NewLocalProxyHTTPClient(65400)
	if err != nil {
		t.Fatalf("创建代理 HTTP 客户端失败：%v", err)
	}
	assertProxyURL(t, withProxy, "http://127.0.0.1:65400")

	direct, err := NewLocalProxyHTTPClient(0)
	if err != nil {
		t.Fatalf("创建直连 HTTP 客户端失败：%v", err)
	}
	assertProxyURL(t, direct, "")
}

// TestNewLocalProxyHTTPClient_RejectsInvalidPort 验证无效端口不会构造可用下载客户端。
func TestNewLocalProxyHTTPClient_RejectsInvalidPort(t *testing.T) {
	t.Parallel()

	if _, err := NewLocalProxyHTTPClient(65536); err == nil {
		t.Fatal("超过范围的端口必须被拒绝")
	}
}

// assertProxyURL 读取 Transport 的代理决策，避免测试真实网络或代理服务。
func assertProxyURL(t *testing.T, client *http.Client, want string) {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport 类型不正确：%T", client.Transport)
	}
	if transport.Proxy == nil {
		if want == "" {
			return
		}
		t.Fatalf("代理地址不正确：got=direct want=%s", want)
	}
	proxyURL, err := transport.Proxy(&http.Request{})
	if err != nil {
		t.Fatalf("读取代理地址失败：%v", err)
	}
	got := ""
	if proxyURL != nil {
		got = proxyURL.String()
	}
	if got != want {
		t.Fatalf("代理地址不正确：got=%s want=%s", got, want)
	}
}
