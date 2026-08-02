package config_test

import (
	"strings"
	"testing"

	"meet-sieve/internal/infra/config"
)

// TestParseLocator_AcceptsOnlyMinimalCurrentSchema 验证 locator 只接受当前版本和绝对工作目录路径。
func TestParseLocator_AcceptsOnlyMinimalCurrentSchema(t *testing.T) {
	locator, err := config.ParseLocator([]byte(`{"schema_version":1,"workspace_path":"/Volumes/Meetings"}`))
	if err != nil {
		t.Fatalf("解析合法 locator 失败：%v", err)
	}
	if locator.SchemaVersion != config.LocatorSchemaVersion || locator.WorkspacePath != "/Volumes/Meetings" {
		t.Fatalf("locator 内容不正确：%#v", locator)
	}
}

// TestParseLocator_RejectsNonMinimalOrUnsafeInput 验证无效 locator 不会被弱类型 JSON 语义接受。
func TestParseLocator_RejectsNonMinimalOrUnsafeInput(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "未知字段", data: `{"schema_version":1,"workspace_path":"/workspace","database_id":"ignored"}`},
		{name: "重复字段", data: `{"schema_version":1,"workspace_path":"/workspace","workspace_path":"/other"}`},
		{name: "缺少版本", data: `{"workspace_path":"/workspace"}`},
		{name: "错误类型", data: `{"schema_version":"1","workspace_path":"/workspace"}`},
		{name: "尾随内容", data: `{"schema_version":1,"workspace_path":"/workspace"} {}`},
		{name: "高版本", data: `{"schema_version":2,"workspace_path":"/workspace"}`},
		{name: "相对路径", data: `{"schema_version":1,"workspace_path":"workspace"}`},
		{name: "家目录缩写", data: `{"schema_version":1,"workspace_path":"~/workspace"}`},
		{name: "环境变量", data: `{"schema_version":1,"workspace_path":"$HOME/workspace"}`},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.ParseLocator([]byte(test.data))
			if err == nil {
				t.Fatal("无效 locator 必须被拒绝")
			}
			if strings.TrimSpace(err.Error()) == "" {
				t.Fatal("无效 locator 必须提供诊断错误")
			}
		})
	}
}
