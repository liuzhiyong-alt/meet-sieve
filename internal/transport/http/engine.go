// Package http 提供可嵌入应用生命周期的 Gin transport。
package http

import (
	"meet-sieve/internal/app/health"
	infraLogger "meet-sieve/internal/infra/logger"
	guesthttp "meet-sieve/internal/transport/http/guest"
	httpmiddleware "meet-sieve/internal/transport/http/middleware"
	"meet-sieve/internal/transport/http/response"

	"github.com/gin-gonic/gin"
)

// NewEngine 创建未绑定监听端口的 Gin Engine。
func NewEngine(registry *health.Registry, appLogger *infraLogger.AppLogger) *gin.Engine {
	engine := gin.New()
	engine.Use(httpmiddleware.RequestID())
	engine.Use(httpmiddleware.AccessLog(appLogger))
	engine.Use(httpmiddleware.ErrorHandler(appLogger))
	engine.Use(httpmiddleware.Recovery())
	registerRoutes(engine, registry)
	return engine
}

// NewGuestEngine 在基础中间件和 health 之上注册独立 Guest API 模块。
func NewGuestEngine(registry *health.Registry, appLogger *infraLogger.AppLogger, dependencies guesthttp.RouteDependencies) *gin.Engine {
	engine := NewEngine(registry, appLogger)
	guesthttp.RegisterRoutes(engine, dependencies)
	return engine
}

// registerRoutes 注册 Step 0 的最小公开路由。
func registerRoutes(engine *gin.Engine, registry *health.Registry) {
	engine.GET("/health", func(ctx *gin.Context) {
		response.Success(ctx, httpmiddleware.RequestIDFrom(ctx), registry.Get())
	})
}
