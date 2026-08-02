package middleware

import (
	"io"

	"meet-sieve/internal/infra/apperr"

	"github.com/gin-gonic/gin"
)

// Recovery 捕获 handler panic，并交由外层 ErrorHandler 统一响应和记录。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(io.Discard, func(ctx *gin.Context, recovered any) {
		AbortWithError(ctx, apperr.RecoveredPanic(recovered, "http.recovery"))
	})
}
