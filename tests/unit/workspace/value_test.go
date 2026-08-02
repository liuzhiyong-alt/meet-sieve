package workspace_test

import (
	"testing"

	"meet-sieve/internal/domain/workspace"
)

// TestStep1StateValues_RejectUnknownValues 验证工作目录状态不会退化为自由字符串。
func TestStep1StateValues_RejectUnknownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		valid func() bool
		bad   func() bool
	}{
		{
			name:  "bootstrap 阶段",
			valid: func() bool { return workspace.BootstrapPhaseReady.IsValid() },
			bad:   func() bool { return workspace.BootstrapPhase("loading").IsValid() },
		},
		{
			name:  "候选目录种类",
			valid: func() bool { return workspace.CandidateKindMeetSieve.IsValid() },
			bad:   func() bool { return workspace.CandidateKind("existing").IsValid() },
		},
		{
			name:  "候选目录原因",
			valid: func() bool { return workspace.CandidateReasonInstallPathForbidden.IsValid() },
			bad:   func() bool { return workspace.CandidateReason("unknown").IsValid() },
		},
		{
			name:  "schema 状态",
			valid: func() bool { return workspace.SchemaStateUpgradeRequired.IsValid() },
			bad:   func() bool { return workspace.SchemaState("dirty").IsValid() },
		},
		{
			name:  "路径警告",
			valid: func() bool { return workspace.CandidateWarningLowDiskSpace.IsValid() },
			bad:   func() bool { return workspace.CandidateWarning("cloud_sync").IsValid() },
		},
		{
			name:  "可用动作",
			valid: func() bool { return workspace.BootstrapActionRetryDatabaseUpgrade.IsValid() },
			bad:   func() bool { return workspace.BootstrapAction("restore_backup").IsValid() },
		},
		{
			name:  "设置禁用原因",
			valid: func() bool { return workspace.WorkspaceEditDisabledReasonMeetingInProgress.IsValid() },
			bad:   func() bool { return workspace.WorkspaceEditDisabledReason("recording").IsValid() },
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if !test.valid() {
				t.Fatal("技术方案登记值应合法")
			}
			if test.bad() {
				t.Fatal("未登记值必须被拒绝")
			}
		})
	}
}

// TestWorkspaceSettings_SeparatesActiveAndSavedPaths 验证设置值对象分开表达当前与下次启动路径。
func TestWorkspaceSettings_SeparatesActiveAndSavedPaths(t *testing.T) {
	t.Parallel()

	settings := workspace.WorkspaceSettings{
		ActivePath:      "/Volumes/MeetingDisk/current",
		SavedPath:       "/Volumes/MeetingDisk/copied",
		RestartRequired: true,
		Editable:        true,
	}

	if settings.ActivePath == settings.SavedPath {
		t.Fatal("当前路径与已保存路径必须分别保留")
	}
	if !settings.RestartRequired {
		t.Fatal("路径不同时必须明确提示下次启动生效")
	}
	if !settings.Editable || settings.DisabledReason != workspace.WorkspaceEditDisabledReasonNone {
		t.Fatalf("可编辑设置不应带禁用原因：%+v", settings)
	}
	if !settings.IsValid() {
		t.Fatal("合法工作目录设置应通过一致性校验")
	}

	blocked := settings
	blocked.Editable = false
	blocked.DisabledReason = workspace.WorkspaceEditDisabledReasonMeetingInProgress
	if !blocked.IsValid() {
		t.Fatal("会议进行中的禁用设置应通过一致性校验")
	}

	blocked.DisabledReason = workspace.WorkspaceEditDisabledReason("recording")
	if blocked.IsValid() {
		t.Fatal("未知禁用原因不能作为工作目录设置返回")
	}
}
