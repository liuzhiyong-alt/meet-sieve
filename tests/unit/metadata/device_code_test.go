package metadata_test

import (
	"testing"

	"meet-sieve/internal/domain/metadata"
)

// TestDeviceCode_UsesRegisteredAlphabet 验证设备码固定为四位且拒绝容易混淆或非法的字符。
func TestDeviceCode_UsesRegisteredAlphabet(t *testing.T) {
	valid, err := metadata.ParseDeviceCode("AB29")
	if err != nil || valid.String() != "AB29" {
		t.Fatalf("合法设备码解析失败：code=%q err=%v", valid.String(), err)
	}
	for _, value := range []string{"ABC", "ABCDE", "A0CD", "AICD", "ab29"} {
		if _, err := metadata.ParseDeviceCode(value); err == nil {
			t.Fatalf("非法设备码 %q 必须被拒绝", value)
		}
	}
}

// TestDeviceCodeGenerator_ConsumesFixedReader 验证设备码生成可由外部随机边界确定性驱动。
func TestDeviceCodeGenerator_ConsumesFixedReader(t *testing.T) {
	code, err := metadata.NewDeviceCodeGenerator(metadata.FixedRandomSource{0, 1, 24, 30}).New()
	if err != nil {
		t.Fatalf("生成设备码失败：%v", err)
	}
	if code.String() != "AB28" {
		t.Fatalf("设备码不正确：%q", code.String())
	}
}

// TestNewRandomDeviceCode 验证生产设备码由安全随机源生成，且始终符合已登记字符集。
func TestNewRandomDeviceCode(t *testing.T) {
	code, err := metadata.NewRandomDeviceCode()
	if err != nil {
		t.Fatalf("生成随机设备码失败：%v", err)
	}
	if _, err := metadata.ParseDeviceCode(code.String()); err != nil {
		t.Fatalf("随机设备码不符合字符集：code=%q err=%v", code.String(), err)
	}
}
