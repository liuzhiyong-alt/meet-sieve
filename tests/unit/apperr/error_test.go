package apperr_test

import (
	"errors"
	"testing"

	"meet-sieve/internal/infra/apperr"

	"go.uber.org/zap/zapcore"
)

// TestNormalizeUnknownError_HidesCause 验证未知内部错误不会把底层信息暴露给调用方。
func TestNormalizeUnknownError_HidesCause(t *testing.T) {
	t.Parallel()

	result := apperr.Normalize(errors.New("sqlite password=secret failed"))

	if result.Code != apperr.CodeInternal.Value {
		t.Fatalf("错误码不正确：got %d, want %d", result.Code, apperr.CodeInternal.Value)
	}
	if result.Message != apperr.CodeInternal.Message {
		t.Fatalf("用户提示泄漏或不稳定：got %q", result.Message)
	}
	if result.Cause == nil {
		t.Fatal("内部排障 cause 不应丢失")
	}
	if result.Op != "unclassified" {
		t.Fatalf("未知错误操作名不正确：got %q", result.Op)
	}
}

// TestSysError_PreservesCauseAndContext 验证系统错误保留 cause、操作名和脱敏排障字段。
func TestSysError_PreservesCauseAndContext(t *testing.T) {
	t.Parallel()

	cause := errors.New("database unavailable")
	result := apperr.Sys(
		cause,
		apperr.WithOp("meeting.create.database"),
		apperr.WithField("meeting_id", "meeting-1"),
	)

	if !errors.Is(result, cause) {
		t.Fatal("系统错误必须保留原始 cause")
	}
	if result.Code != apperr.CodeInternal.Value || result.Kind != apperr.KindSystem {
		t.Fatalf("系统错误分类不正确：got code=%d kind=%q", result.Code, result.Kind)
	}
	if result.Op != "meeting.create.database" {
		t.Fatalf("操作名不正确：got %q", result.Op)
	}
	if result.Fields["meeting_id"] != "meeting-1" {
		t.Fatalf("排障字段不正确：got %#v", result.Fields)
	}
}

// TestDependencyError_UsesRegisteredDefaults 验证依赖错误使用集中登记的默认语义。
func TestDependencyError_UsesRegisteredDefaults(t *testing.T) {
	t.Parallel()

	result := apperr.Dependency(
		apperr.CodeDependencyTimeout,
		errors.New("codex timeout"),
		apperr.WithOp("codex.initialize"),
	)

	if result.Code != apperr.CodeDependencyTimeout.Value {
		t.Fatalf("错误码不正确：got %d", result.Code)
	}
	if result.Kind != apperr.KindDependency || !result.Retryable {
		t.Fatalf("依赖错误属性不正确：kind=%q retryable=%v", result.Kind, result.Retryable)
	}
}

// TestStep1Codes_ExposeStableTransportSemantics 验证 Step 1 错误同时保留数值码和稳定字符串码。
func TestStep1Codes_ExposeStableTransportSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		code      apperr.Code
		errorCode string
		retryable bool
	}{
		{name: "路径无效", code: apperr.CodeWorkspacePathInvalid, errorCode: "WORKSPACE_PATH_INVALID"},
		{name: "非 MeetSieve 非空目录", code: apperr.CodeWorkspaceNotEmpty, errorCode: "WORKSPACE_NOT_EMPTY"},
		{name: "不支持网络卷", code: apperr.CodeWorkspaceUnsupportedVolume, errorCode: "WORKSPACE_UNSUPPORTED_VOLUME"},
		{name: "安装目录禁止", code: apperr.CodeWorkspaceInstallPathForbidden, errorCode: "WORKSPACE_INSTALL_PATH_FORBIDDEN"},
		{name: "目录不可写", code: apperr.CodeWorkspaceNotWritable, errorCode: "WORKSPACE_NOT_WRITABLE", retryable: true},
		{name: "数据库缺失", code: apperr.CodeWorkspaceDatabaseMissing, errorCode: "WORKSPACE_DATABASE_MISSING"},
		{name: "数据库无效", code: apperr.CodeWorkspaceDatabaseInvalid, errorCode: "WORKSPACE_DATABASE_INVALID"},
		{name: "schema 过新", code: apperr.CodeWorkspaceSchemaNewer, errorCode: "WORKSPACE_SCHEMA_NEWER"},
		{name: "会议中禁止修改", code: apperr.CodeWorkspaceChangeBlocked, errorCode: "WORKSPACE_CHANGE_BLOCKED", retryable: true},
		{name: "locator 无效", code: apperr.CodeLocatorInvalid, errorCode: "LOCATOR_INVALID"},
		{name: "locator 写入失败", code: apperr.CodeLocatorWriteFailed, errorCode: "LOCATOR_WRITE_FAILED", retryable: true},
		{name: "数据库繁忙", code: apperr.CodeDatabaseBusy, errorCode: "DATABASE_BUSY", retryable: true},
		{name: "数据库备份失败", code: apperr.CodeDatabaseBackupFailed, errorCode: "DATABASE_BACKUP_FAILED", retryable: true},
		{name: "数据库升级失败", code: apperr.CodeDatabaseMigrationFailed, errorCode: "DATABASE_MIGRATION_FAILED", retryable: true},
		{name: "数据库完整性失败", code: apperr.CodeDatabaseIntegrityFailed, errorCode: "DATABASE_INTEGRITY_FAILED"},
		{name: "升级空间不足", code: apperr.CodeDatabaseDiskSpaceLow, errorCode: "DATABASE_DISK_SPACE_LOW", retryable: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := apperr.Biz(test.code)

			if result.Code != test.code.Value {
				t.Fatalf("数值码不正确：got %d, want %d", result.Code, test.code.Value)
			}
			if result.ErrorCode != test.errorCode {
				t.Fatalf("稳定错误码不正确：got %q, want %q", result.ErrorCode, test.errorCode)
			}
			if result.Message == "" {
				t.Fatal("安全用户文案不能为空")
			}
			if result.Retryable != test.retryable {
				t.Fatalf("可重试属性不正确：got %v, want %v", result.Retryable, test.retryable)
			}
		})
	}
}

// TestClassifyLog_UsesErrorKind 验证日志等级由统一错误分类决定。
func TestClassifyLog_UsesErrorKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		err   *apperr.AppError
		level zapcore.Level
	}{
		{name: "用户取消", err: apperr.Biz(apperr.CodeCanceled), level: zapcore.InfoLevel},
		{name: "业务冲突", err: apperr.Biz(apperr.CodeConflict), level: zapcore.WarnLevel},
		{name: "依赖失败", err: apperr.Dependency(apperr.CodeDependency, errors.New("downstream")), level: zapcore.ErrorLevel},
		{name: "系统失败", err: apperr.Sys(errors.New("internal")), level: zapcore.ErrorLevel},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if level := apperr.ClassifyLog(test.err); level != test.level {
				t.Fatalf("日志等级不正确：got %s, want %s", level, test.level)
			}
		})
	}
}
