package guest

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

const guestWebCSP = "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"

// registerWebRoutes 只公开 Guest 构建产物，不把桌面应用资源暴露给局域网。
func registerWebRoutes(engine *gin.Engine, assets fs.FS) {
	if engine == nil || assets == nil {
		return
	}
	engine.GET("/join", guestIndexHandler(assets))
	engine.GET("/guest.html", guestIndexHandler(assets))
	engine.GET("/guest-assets/*filepath", guestAssetHandler(assets))
}

// guestIndexHandler 返回不缓存且带严格 CSP 的 Guest 单页入口。
func guestIndexHandler(assets fs.FS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		content, err := fs.ReadFile(assets, "guest.html")
		if err != nil {
			ctx.Status(http.StatusNotFound)
			return
		}
		setGuestWebHeaders(ctx)
		ctx.Header("Cache-Control", "no-store")
		ctx.Data(http.StatusOK, "text/html; charset=utf-8", content)
	}
}

// guestAssetHandler 返回哈希静态资源，并拒绝目录、点文件和非 assets 路径。
func guestAssetHandler(assets fs.FS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requested := path.Clean(strings.TrimPrefix(ctx.Param("filepath"), "/"))
		if requested == "." || !strings.HasPrefix(requested, "assets/") || path.Base(requested)[0] == '.' {
			ctx.Status(http.StatusNotFound)
			return
		}
		content, err := fs.ReadFile(assets, requested)
		if err != nil {
			ctx.Status(http.StatusNotFound)
			return
		}
		setGuestWebHeaders(ctx)
		ctx.Header("Cache-Control", "public, max-age=31536000, immutable")
		ctx.Data(http.StatusOK, contentTypeForAsset(requested), content)
	}
}

// setGuestWebHeaders 设置访客静态页面的浏览器安全边界。
func setGuestWebHeaders(ctx *gin.Context) {
	ctx.Header("Content-Security-Policy", guestWebCSP)
	ctx.Header("X-Content-Type-Options", "nosniff")
	ctx.Header("Referrer-Policy", "no-referrer")
	ctx.Header("X-Frame-Options", "DENY")
}

// contentTypeForAsset 只映射构建产物允许出现的资源类型。
func contentTypeForAsset(name string) string {
	switch path.Ext(name) {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}
