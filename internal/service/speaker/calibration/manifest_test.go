package calibration

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/port"
)

// TestParseManifestRejectsSingleSpeaker 验证单人数据不能伪装成正式校准集。
func TestParseManifestRejectsSingleSpeaker(t *testing.T) {
	data := []byte(`{
  "schema_version": 1,
  "profile_id": "test",
  "calibration_record": "docs/calibration/test.md",
  "evidence": {"min_evidence_ms": 1, "target_evidence_ms": 1},
  "identity": {"min_score": 0.9, "min_margin": 0.1},
  "unknown_cluster": {"min_score": 0.9, "min_margin": 0.1},
  "samples": [
    {"speaker_id":"a","session_id":"enroll","role":"enrollment","path":"a0.wav"},
    {"speaker_id":"a","session_id":"eval","role":"evaluation","path":"a1.wav"},
    {"speaker_id":"a","session_id":"eval","role":"evaluation","path":"a2.wav"}
  ]
}`)
	if _, err := ParseManifest(data); err == nil || !strings.Contains(err.Error(), "至少需要两位") {
		t.Fatalf("应拒绝单人校准集，实际错误：%v", err)
	}
}

// TestRunProducesProfile 验证独立双人数据通过生产 matcher 与 clusterer 后生成档案。
func TestRunProducesProfile(t *testing.T) {
	directory := t.TempDir()
	manifest := validManifest()
	for index, sample := range manifest.Samples {
		amplitude := int16(1000 + index)
		if sample.SpeakerID == "b" {
			amplitude = -amplitude
		}
		writeTestWAV(t, filepath.Join(directory, sample.Path), amplitude)
	}
	result, err := Run(context.Background(), manifest, directory, signEncoder{})
	if err != nil {
		t.Fatalf("运行校准失败：%v", err)
	}
	if result.Metrics.IdentityCorrect != 4 || result.Metrics.ClusterFalseMerge != 0 || result.Metrics.ClusterFalseSplit != 0 {
		t.Fatalf("校准指标不正确：%+v", result.Metrics)
	}
	if len(result.Samples) != 6 || !result.Samples[1].Matched || result.Profile.ProfileID != manifest.ProfileID {
		t.Fatalf("校准结果不完整：%+v", result)
	}
}

// TestRunRejectsDuplicateAudio 验证改名后的同一音频也不能跨集合复用。
func TestRunRejectsDuplicateAudio(t *testing.T) {
	directory := t.TempDir()
	manifest := validManifest()
	for _, sample := range manifest.Samples {
		writeTestWAV(t, filepath.Join(directory, sample.Path), 1000)
	}
	if _, err := Run(context.Background(), manifest, directory, signEncoder{}); err == nil || !strings.Contains(err.Error(), "内容重复") {
		t.Fatalf("应拒绝重复音频，实际错误：%v", err)
	}
}

// TestRunRejectsOutOfSetFalseAccept 验证真实说话人不在候选成员时不能被误认。
func TestRunRejectsOutOfSetFalseAccept(t *testing.T) {
	directory := t.TempDir()
	manifest := validManifest()
	manifest.Identity.MinScore = 0
	for index, sample := range manifest.Samples {
		amplitude := int16(1000 + index)
		if sample.SpeakerID == "b" {
			amplitude = -amplitude
		}
		writeTestWAV(t, filepath.Join(directory, sample.Path), amplitude)
	}
	result, err := Run(context.Background(), manifest, directory, signEncoder{})
	if err == nil || result.Metrics.IdentityFalseAccept == 0 {
		t.Fatalf("应拒绝 out-of-set 误认，metrics=%+v err=%v", result.Metrics, err)
	}
}

// validManifest 返回覆盖两位说话人的最小正式校准清单。
func validManifest() Manifest {
	return Manifest{
		SchemaVersion: 1, ProfileID: "test-profile", CalibrationRecord: "docs/calibration/test.md",
		Evidence:       speakerdomain.EvidenceProfile{MinEvidenceMS: 1, TargetEvidenceMS: 1},
		Identity:       speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1},
		UnknownCluster: speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1},
		Samples: []Sample{
			{SpeakerID: "a", SessionID: "a-enroll", Role: RoleEnrollment, Path: "a0.wav"},
			{SpeakerID: "a", SessionID: "a-eval", Role: RoleEvaluation, Path: "a1.wav"},
			{SpeakerID: "b", SessionID: "b-enroll", Role: RoleEnrollment, Path: "b0.wav"},
			{SpeakerID: "b", SessionID: "b-eval", Role: RoleEvaluation, Path: "b1.wav"},
			{SpeakerID: "a", SessionID: "a-eval", Role: RoleEvaluation, Path: "a2.wav"},
			{SpeakerID: "b", SessionID: "b-eval", Role: RoleEvaluation, Path: "b2.wav"},
		},
	}
}

type signEncoder struct{}

// Encode 根据首个样本符号生成两个正交 embedding，模拟可分离的真实 encoder 输出。
func (signEncoder) Encode(_ context.Context, pcm port.AudioPCM) (port.Embedding, error) {
	if len(pcm.Samples) == 0 {
		return nil, fmt.Errorf("PCM 为空")
	}
	if pcm.Samples[0] >= 0 {
		return port.Embedding{1, 0}, nil
	}
	return port.Embedding{0, 1}, nil
}

// ModelInfo 返回测试 encoder 的固定模型身份。
func (signEncoder) ModelInfo() port.ModelInfo {
	return port.ModelInfo{ID: "test", Version: "1", SHA256: strings.Repeat("a", 64), Dimension: 2}
}

// Close 满足 VoiceEncoder 生命周期接口。
func (signEncoder) Close() error { return nil }

// writeTestWAV 写入一个至少 2ms 的 16kHz 单声道 PCM WAV。
func writeTestWAV(t *testing.T, path string, amplitude int16) {
	t.Helper()
	const sampleCount = 32
	dataSize := sampleCount * 2
	content := make([]byte, 44+dataSize)
	copy(content[0:4], "RIFF")
	binary.LittleEndian.PutUint32(content[4:8], uint32(36+dataSize))
	copy(content[8:12], "WAVE")
	copy(content[12:16], "fmt ")
	binary.LittleEndian.PutUint32(content[16:20], 16)
	binary.LittleEndian.PutUint16(content[20:22], 1)
	binary.LittleEndian.PutUint16(content[22:24], 1)
	binary.LittleEndian.PutUint32(content[24:28], 16000)
	binary.LittleEndian.PutUint32(content[28:32], 32000)
	binary.LittleEndian.PutUint16(content[32:34], 2)
	binary.LittleEndian.PutUint16(content[34:36], 16)
	copy(content[36:40], "data")
	binary.LittleEndian.PutUint32(content[40:44], uint32(dataSize))
	for index := 0; index < sampleCount; index++ {
		binary.LittleEndian.PutUint16(content[44+index*2:], uint16(amplitude))
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入测试 WAV 失败：%v", err)
	}
}
