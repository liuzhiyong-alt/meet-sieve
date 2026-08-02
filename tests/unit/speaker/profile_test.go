package speaker_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
	speakerservice "meet-sieve/internal/service/speaker"
)

var expectedModel = speakerdomain.ModelIdentity{
	ID: "iic/speech_campplus_sv_zh-cn_16k-common", Version: "1.0.0-ms1",
	SHA256: "57f6b2439b06fc453ed36159a44b97693610fb0a67c0dafd696d54e24d2b1ae1", Dimension: 192,
}

// TestParseMatchingProfile_AcceptsStrictTemporaryProfile 验证临时测试档案可解析但不落入生产 models 目录。
func TestParseMatchingProfile_AcceptsStrictTemporaryProfile(t *testing.T) {
	profile, err := speakerdomain.ParseMatchingProfile([]byte(validProfileJSON()), expectedModel)
	if err != nil {
		t.Fatalf("解析合法测试档案失败：%v", err)
	}
	if profile.ProfileID != "test-room-alpha1" || profile.Evidence.TargetEvidenceMS != 8000 {
		t.Fatalf("档案字段映射错误：%+v", profile)
	}
}

// TestParseMatchingProfile_RejectsInvalidContracts 验证未知、重复、缺失、越界和尾随字段均被拒绝。
func TestParseMatchingProfile_RejectsInvalidContracts(t *testing.T) {
	tests := map[string]string{
		"unknown":        strings.Replace(validProfileJSON(), `"schema_version":1`, `"schema_version":1,"extra":true`, 1),
		"duplicate":      strings.Replace(validProfileJSON(), `"min_score":0.7`, `"min_score":0.7,"min_score":0.8`, 1),
		"missing":        strings.Replace(validProfileJSON(), `,"calibration_record":"docs/spec/test.md"`, "", 1),
		"missing_score":  strings.Replace(validProfileJSON(), `"min_score":0.7,`, "", 1),
		"evidence_zero":  strings.Replace(validProfileJSON(), `"min_evidence_ms":3000`, `"min_evidence_ms":0`, 1),
		"evidence_order": strings.Replace(validProfileJSON(), `"target_evidence_ms":8000`, `"target_evidence_ms":2000`, 1),
		"evidence_limit": strings.Replace(validProfileJSON(), `"target_evidence_ms":8000`, `"target_evidence_ms":120001`, 1),
		"score":          strings.Replace(validProfileJSON(), `"min_score":0.7`, `"min_score":1.1`, 1),
		"margin":         strings.Replace(validProfileJSON(), `"min_margin":0.1`, `"min_margin":2.1`, 1),
		"trailing":       validProfileJSON() + `{}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := speakerdomain.ParseMatchingProfile([]byte(content), expectedModel); err == nil {
				t.Fatal("非法档案必须被拒绝")
			}
		})
	}
}

// TestParseMatchingProfile_RejectsModelMismatch 验证模型四元组任一不一致均返回稳定领域原因。
func TestParseMatchingProfile_RejectsModelMismatch(t *testing.T) {
	mutations := []speakerdomain.ModelIdentity{
		{ID: "other", Version: expectedModel.Version, SHA256: expectedModel.SHA256, Dimension: expectedModel.Dimension},
		{ID: expectedModel.ID, Version: "other", SHA256: expectedModel.SHA256, Dimension: expectedModel.Dimension},
		{ID: expectedModel.ID, Version: expectedModel.Version, SHA256: strings.Repeat("a", 64), Dimension: expectedModel.Dimension},
		{ID: expectedModel.ID, Version: expectedModel.Version, SHA256: expectedModel.SHA256, Dimension: 256},
	}
	for _, model := range mutations {
		if _, err := speakerdomain.ParseMatchingProfile([]byte(validProfileJSON()), model); !errors.Is(err, speakerdomain.ErrProfileMismatch) {
			t.Fatalf("四元组不一致必须返回 mismatch：model=%+v err=%v", model, err)
		}
	}
}

// TestLoadMatchingProfile_MapsMissingFileToStableCode 验证正式档案缺失只关闭自动识别。
func TestLoadMatchingProfile_MapsMissingFileToStableCode(t *testing.T) {
	_, err := speakerservice.LoadMatchingProfile(filepath.Join(t.TempDir(), "voice-matching-profile.json"), expectedModel)
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeSpeakerProfileMissing.ErrorCode {
		t.Fatalf("缺失档案错误码不正确：%v", err)
	}
}

// TestLoadMatchingProfile_MapsInvalidFileToMismatchCode 验证档案内容异常不暴露原始 JSON。
func TestLoadMatchingProfile_MapsInvalidFileToMismatchCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice-matching-profile.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("准备非法档案失败：%v", err)
	}
	_, err := speakerservice.LoadMatchingProfile(path, expectedModel)
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeSpeakerProfileMismatch.ErrorCode {
		t.Fatalf("非法档案错误码不正确：%v", err)
	}
}

// validProfileJSON 返回只用于 parser 测试的显式临时阈值，不代表生产校准结论。
func validProfileJSON() string {
	return `{"schema_version":1,"profile_id":"test-room-alpha1","model":{"id":"iic/speech_campplus_sv_zh-cn_16k-common","version":"1.0.0-ms1","sha256":"57f6b2439b06fc453ed36159a44b97693610fb0a67c0dafd696d54e24d2b1ae1","dimension":192},"evidence":{"min_evidence_ms":3000,"target_evidence_ms":8000},"identity":{"min_score":0.7,"min_margin":0.1},"unknown_cluster":{"min_score":0.65,"min_margin":0.08},"calibration_record":"docs/spec/test.md"}`
}
