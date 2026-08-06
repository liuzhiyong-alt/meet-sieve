package guest

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

// TestRegisterWebRoutes 证明 LAN 只公开 Guest 构建产物并设置浏览器安全头。
func TestRegisterWebRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{
		"guest.html":         &fstest.MapFile{Data: []byte("<main>Guest</main>")},
		"assets/guest.js":    &fstest.MapFile{Data: []byte("export{}")},
		"desktop/index.html": &fstest.MapFile{Data: []byte("desktop")},
	}
	engine := gin.New()
	registerWebRoutes(engine, fs.FS(assets))

	index := performWebRequest(engine, "/join")
	if index.Code != http.StatusOK || index.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("访客入口响应不正确: status=%d cache=%q", index.Code, index.Header().Get("Cache-Control"))
	}
	if index.Header().Get("Content-Security-Policy") != guestWebCSP {
		t.Fatalf("访客入口缺少严格 CSP: %q", index.Header().Get("Content-Security-Policy"))
	}

	asset := performWebRequest(engine, "/guest-assets/assets/guest.js")
	if asset.Code != http.StatusOK || asset.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("访客资源响应不正确: status=%d type=%q", asset.Code, asset.Header().Get("Content-Type"))
	}
	if response := performWebRequest(engine, "/guest-assets/../desktop/index.html"); response.Code != http.StatusNotFound {
		t.Fatalf("不得通过 Guest 路由读取桌面资源: %d", response.Code)
	}
}

// TestValidateWebAssets 验证构建产物缺少入口或被入口引用的资源时不能被视为可服务。
func TestValidateWebAssets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		assets fstest.MapFS
		wantOK bool
	}{
		{name: "complete", assets: fstest.MapFS{"guest.html": &fstest.MapFile{Data: []byte(`<script src="/guest-assets/assets/guest.js"></script>`)}, "assets/guest.js": &fstest.MapFile{Data: []byte("export{}")}}, wantOK: true},
		{name: "missing entry", assets: fstest.MapFS{}, wantOK: false},
		{name: "missing referenced asset", assets: fstest.MapFS{"guest.html": &fstest.MapFile{Data: []byte(`<link href="/guest-assets/assets/guest.css">`)}}, wantOK: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := ValidateWebAssets(test.assets)
			if (err == nil) != test.wantOK {
				t.Fatalf("资源预检结果错误：err=%v", err)
			}
		})
	}
}

// performWebRequest 执行无网络监听的 Guest 静态资源请求。
func performWebRequest(handler http.Handler, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
