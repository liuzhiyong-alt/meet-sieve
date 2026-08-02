package apperr

// Kind 表示错误的稳定分类，用于日志等级和重试决策。
type Kind string

const (
	// KindValidation 表示参数或输入校验失败。
	KindValidation Kind = "validation"
	// KindBusiness 表示可预期的业务状态失败。
	KindBusiness Kind = "business"
	// KindDependency 表示下游依赖失败。
	KindDependency Kind = "dependency"
	// KindSystem 表示内部系统失败。
	KindSystem Kind = "system"
	// KindCanceled 表示用户取消。
	KindCanceled Kind = "canceled"
)

// AppError 保存对外稳定信息和仅用于内部排障的错误上下文。
// Cause 和 Fields 永不直接序列化到 Wails 或 HTTP 响应。
type AppError struct {
	// Code 是对外稳定错误码。
	Code int
	// ErrorCode 是供客户端稳定识别错误类型的字符串码。
	ErrorCode string
	// Kind 是错误分类。
	Kind Kind
	// Message 是安全用户提示。
	Message string
	// Op 是层.动作.步骤格式的内部操作名。
	Op string
	// Cause 是只允许进入脱敏日志的底层错误。
	Cause error
	// Fields 是调用方确认已脱敏的排障字段。
	Fields map[string]string
	// Retryable 表示当前失败是否适合重试。
	Retryable bool
}

// Error 返回安全的用户提示，避免错误字符串经由调用链泄漏内部 cause。
func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap 返回原始 cause，使错误链可被日志和 errors.Is/As 使用。
func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Option 定义 AppError 的可选排障上下文。
type Option func(*AppError)

// WithOp 设置层.动作.步骤格式的内部操作名。
func WithOp(op string) Option {
	return func(appErr *AppError) {
		appErr.Op = op
	}
}

// WithField 添加一个已经脱敏的排障字段。
func WithField(key string, value string) Option {
	return func(appErr *AppError) {
		if key == "" {
			return
		}
		if appErr.Fields == nil {
			appErr.Fields = make(map[string]string)
		}
		appErr.Fields[key] = value
	}
}

// WithRetryable 覆盖错误码登记的默认重试属性。
func WithRetryable(retryable bool) Option {
	return func(appErr *AppError) {
		appErr.Retryable = retryable
	}
}
