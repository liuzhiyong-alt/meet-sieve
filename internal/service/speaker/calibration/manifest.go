// Package calibration 使用真实、独立标注的音频生成说话人匹配档案。
package calibration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strings"

	speakerdomain "meet-sieve/internal/domain/speaker"
)

const manifestSchemaVersion = 1

// SampleRole 区分声纹录入样本与独立评估样本。
type SampleRole string

const (
	// RoleEnrollment 表示只用于候选声纹库的录入样本。
	RoleEnrollment SampleRole = "enrollment"
	// RoleEvaluation 表示只用于阈值验收的评估样本。
	RoleEvaluation SampleRole = "evaluation"
)

// Sample 描述一段已人工确认归属的真实 WAV。
type Sample struct {
	SpeakerID string     `json:"speaker_id"`
	SessionID string     `json:"session_id"`
	Role      SampleRole `json:"role"`
	Path      string     `json:"path"`
}

// Manifest 是一次可复现校准的输入契约，阈值必须显式给出。
type Manifest struct {
	SchemaVersion     int                           `json:"schema_version"`
	ProfileID         string                        `json:"profile_id"`
	CalibrationRecord string                        `json:"calibration_record"`
	Evidence          speakerdomain.EvidenceProfile `json:"evidence"`
	Identity          speakerdomain.ScoreThresholds `json:"identity"`
	UnknownCluster    speakerdomain.ScoreThresholds `json:"unknown_cluster"`
	Samples           []Sample                      `json:"samples"`
}

// ParseManifest 严格解析校准清单并验证数据集独立性。
func ParseManifest(data []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("解析校准清单失败：%w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate 要求至少两人，且每人的录入与评估来自不同会话。
func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("校准清单 schema_version 不受支持")
	}
	if strings.TrimSpace(manifest.ProfileID) == "" || strings.TrimSpace(manifest.CalibrationRecord) == "" {
		return fmt.Errorf("校准清单缺少 profile_id 或 calibration_record")
	}
	if filepath.IsAbs(manifest.CalibrationRecord) || filepath.Ext(manifest.CalibrationRecord) != ".md" ||
		strings.HasPrefix(filepath.Clean(manifest.CalibrationRecord), ".."+string(filepath.Separator)) {
		return fmt.Errorf("calibration_record 必须是仓库内 Markdown 相对路径")
	}
	if manifest.Evidence.MinEvidenceMS <= 0 || manifest.Evidence.TargetEvidenceMS < manifest.Evidence.MinEvidenceMS ||
		manifest.Evidence.TargetEvidenceMS > speakerdomain.MaxEvidenceDurationMS {
		return fmt.Errorf("校准清单 evidence 不合法")
	}
	if err := validateThresholds(manifest.Identity); err != nil {
		return fmt.Errorf("identity 阈值不合法：%w", err)
	}
	if err := validateThresholds(manifest.UnknownCluster); err != nil {
		return fmt.Errorf("unknown_cluster 阈值不合法：%w", err)
	}
	return validateSamples(manifest.Samples)
}

// validateSamples 校验角色、路径唯一性和每位说话人的跨会话覆盖。
func validateSamples(samples []Sample) error {
	type coverage struct {
		enrollment map[string]struct{}
		evaluation map[string]struct{}
		evalCount  int
	}
	bySpeaker := map[string]*coverage{}
	paths := map[string]struct{}{}
	for index, sample := range samples {
		if strings.TrimSpace(sample.SpeakerID) == "" || strings.TrimSpace(sample.SessionID) == "" || strings.TrimSpace(sample.Path) == "" {
			return fmt.Errorf("samples[%d] 字段不完整", index)
		}
		if sample.Role != RoleEnrollment && sample.Role != RoleEvaluation {
			return fmt.Errorf("samples[%d] role 不受支持", index)
		}
		cleaned := filepath.Clean(sample.Path)
		if _, exists := paths[cleaned]; exists {
			return fmt.Errorf("音频路径重复：%s", sample.Path)
		}
		paths[cleaned] = struct{}{}
		item := bySpeaker[sample.SpeakerID]
		if item == nil {
			item = &coverage{enrollment: map[string]struct{}{}, evaluation: map[string]struct{}{}}
			bySpeaker[sample.SpeakerID] = item
		}
		if sample.Role == RoleEnrollment {
			item.enrollment[sample.SessionID] = struct{}{}
		} else {
			item.evaluation[sample.SessionID] = struct{}{}
			item.evalCount++
		}
	}
	if len(bySpeaker) < 2 {
		return fmt.Errorf("正式校准至少需要两位说话人")
	}
	for speakerID, item := range bySpeaker {
		if len(item.enrollment) == 0 || item.evalCount < 2 {
			return fmt.Errorf("说话人 %s 至少需要一段录入音频和两段评估音频", speakerID)
		}
		for sessionID := range item.enrollment {
			if _, overlaps := item.evaluation[sessionID]; overlaps {
				return fmt.Errorf("说话人 %s 的录入和评估会话不能重叠", speakerID)
			}
		}
	}
	return nil
}

// SpeakerIDs 返回排序后的匿名说话人 ID，保证报告稳定。
func (manifest Manifest) SpeakerIDs() []string {
	seen := map[string]struct{}{}
	for _, sample := range manifest.Samples {
		seen[sample.SpeakerID] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for speakerID := range seen {
		result = append(result, speakerID)
	}
	sort.Strings(result)
	return result
}

// validateThresholds 只检查显式阈值的数学定义域，不提供默认值。
func validateThresholds(value speakerdomain.ScoreThresholds) error {
	if math.IsNaN(value.MinScore) || math.IsInf(value.MinScore, 0) ||
		math.IsNaN(value.MinMargin) || math.IsInf(value.MinMargin, 0) ||
		value.MinScore < -1 || value.MinScore > 1 || value.MinMargin < 0 || value.MinMargin > 2 {
		return fmt.Errorf("分数超出 cosine 定义域")
	}
	return nil
}

// requireEOF 拒绝 JSON 根对象后的额外内容。
func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("校准清单包含多个 JSON 值")
		}
		return fmt.Errorf("校准清单尾部不合法：%w", err)
	}
	return nil
}
