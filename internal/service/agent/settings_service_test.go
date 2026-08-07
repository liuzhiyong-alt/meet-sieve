package agent

import "testing"

// TestValidateExecutableSetting_DefersBareCommandLookup 验证保存设置不依赖 Finder 或 Explorer 的旧 PATH。
func TestValidateExecutableSetting_DefersBareCommandLookup(t *testing.T) {
	t.Parallel()

	value, err := validateExecutableSetting("codex")
	if err != nil || value == nil || *value != "codex" {
		t.Fatalf("裸 Codex 命令应交给 Launcher 解析：value=%v err=%v", value, err)
	}
}

// TestValidateExecutableSetting_RejectsCommandArguments 验证设置仍只接受单个入口。
func TestValidateExecutableSetting_RejectsCommandArguments(t *testing.T) {
	t.Parallel()

	if _, err := validateExecutableSetting("codex --version"); err == nil {
		t.Fatal("Codex 设置不能携带命令参数")
	}
}

// TestValidateProxyPort 验证零值禁用代理，只有合法端口可以持久化。
func TestValidateProxyPort(t *testing.T) {
	t.Parallel()

	if value, err := validateProxyPort(0); err != nil || value != nil {
		t.Fatalf("零值应表示直连：value=%v err=%v", value, err)
	}
	value, err := validateProxyPort(65400)
	if err != nil || value == nil || *value != 65400 {
		t.Fatalf("合法代理端口校验失败：value=%v err=%v", value, err)
	}
	if _, err := validateProxyPort(65536); err == nil {
		t.Fatal("超出范围的代理端口必须失败")
	}
}
