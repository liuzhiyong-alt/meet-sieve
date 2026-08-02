package speaker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
)

const (
	matchingProfileSchemaVersion = 1
	// MaxEvidenceDurationMS 是 rolling buffer 与单次说话人推理共享的 120 秒上限。
	MaxEvidenceDurationMS = 120_000
)

var (
	// ErrProfileInvalid 表示档案结构或数值不符合严格契约。
	ErrProfileInvalid = errors.New("说话人匹配档案无效")
	// ErrProfileMismatch 表示档案绑定的模型四元组与当前模型不一致。
	ErrProfileMismatch = errors.New("说话人匹配档案模型不匹配")
)

// ModelIdentity 描述说话人匹配必须精确绑定的模型四元组。
type ModelIdentity struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Dimension int    `json:"dimension"`
}

// EvidenceProfile 约束单个 track 可用于一次推理的证据时长。
type EvidenceProfile struct {
	MinEvidenceMS    int `json:"min_evidence_ms"`
	TargetEvidenceMS int `json:"target_evidence_ms"`
}

// ScoreThresholds 描述 cosine 绝对阈值和 top-1/top-2 最小差值。
type ScoreThresholds struct {
	MinScore  float64 `json:"min_score"`
	MinMargin float64 `json:"min_margin"`
}

// MatchingProfile 是经过真实校准且精确绑定模型身份的自动识别档案。
type MatchingProfile struct {
	SchemaVersion     int             `json:"schema_version"`
	ProfileID         string          `json:"profile_id"`
	Model             ModelIdentity   `json:"model"`
	Evidence          EvidenceProfile `json:"evidence"`
	Identity          ScoreThresholds `json:"identity"`
	UnknownCluster    ScoreThresholds `json:"unknown_cluster"`
	CalibrationRecord string          `json:"calibration_record"`
}

type matchingProfileWire struct {
	SchemaVersion     *int                 `json:"schema_version"`
	ProfileID         *string              `json:"profile_id"`
	Model             *modelIdentityWire   `json:"model"`
	Evidence          *evidenceProfileWire `json:"evidence"`
	Identity          *scoreThresholdsWire `json:"identity"`
	UnknownCluster    *scoreThresholdsWire `json:"unknown_cluster"`
	CalibrationRecord *string              `json:"calibration_record"`
}

type modelIdentityWire struct {
	ID        *string `json:"id"`
	Version   *string `json:"version"`
	SHA256    *string `json:"sha256"`
	Dimension *int    `json:"dimension"`
}

type evidenceProfileWire struct {
	MinEvidenceMS    *int `json:"min_evidence_ms"`
	TargetEvidenceMS *int `json:"target_evidence_ms"`
}

type scoreThresholdsWire struct {
	MinScore  *float64 `json:"min_score"`
	MinMargin *float64 `json:"min_margin"`
}

// ParseMatchingProfile 严格解析校准档案并核对当前模型身份。
func ParseMatchingProfile(data []byte, expected ModelIdentity) (MatchingProfile, error) {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return MatchingProfile{}, fmt.Errorf("%w: %v", ErrProfileInvalid, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire matchingProfileWire
	if err := decoder.Decode(&wire); err != nil {
		return MatchingProfile{}, fmt.Errorf("%w: %v", ErrProfileInvalid, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return MatchingProfile{}, fmt.Errorf("%w: %v", ErrProfileInvalid, err)
	}
	profile, err := wire.build()
	if err != nil {
		return MatchingProfile{}, err
	}
	if err := validateMatchingProfile(profile); err != nil {
		return MatchingProfile{}, err
	}
	if profile.Model != expected {
		return MatchingProfile{}, ErrProfileMismatch
	}
	return profile, nil
}

// build 要求每个标量字段显式出现，避免 JSON 缺失值被 Go 零值伪装为合法阈值。
func (wire matchingProfileWire) build() (MatchingProfile, error) {
	if wire.SchemaVersion == nil || wire.ProfileID == nil || wire.Model == nil || wire.Evidence == nil ||
		wire.Identity == nil || wire.UnknownCluster == nil || wire.CalibrationRecord == nil {
		return MatchingProfile{}, fmt.Errorf("%w: 缺少必填字段", ErrProfileInvalid)
	}
	model, err := wire.Model.build()
	if err != nil {
		return MatchingProfile{}, err
	}
	evidence, err := wire.Evidence.build()
	if err != nil {
		return MatchingProfile{}, err
	}
	identity, err := wire.Identity.build()
	if err != nil {
		return MatchingProfile{}, err
	}
	unknownCluster, err := wire.UnknownCluster.build()
	if err != nil {
		return MatchingProfile{}, err
	}
	return MatchingProfile{
		SchemaVersion: *wire.SchemaVersion, ProfileID: *wire.ProfileID, Model: model, Evidence: evidence,
		Identity: identity, UnknownCluster: unknownCluster, CalibrationRecord: *wire.CalibrationRecord,
	}, nil
}

// build 将完整的 wire 模型身份转换成值对象。
func (wire modelIdentityWire) build() (ModelIdentity, error) {
	if wire.ID == nil || wire.Version == nil || wire.SHA256 == nil || wire.Dimension == nil {
		return ModelIdentity{}, fmt.Errorf("%w: model 缺少必填字段", ErrProfileInvalid)
	}
	return ModelIdentity{ID: *wire.ID, Version: *wire.Version, SHA256: *wire.SHA256, Dimension: *wire.Dimension}, nil
}

// build 将完整的 wire 证据配置转换成值对象。
func (wire evidenceProfileWire) build() (EvidenceProfile, error) {
	if wire.MinEvidenceMS == nil || wire.TargetEvidenceMS == nil {
		return EvidenceProfile{}, fmt.Errorf("%w: evidence 缺少必填字段", ErrProfileInvalid)
	}
	return EvidenceProfile{MinEvidenceMS: *wire.MinEvidenceMS, TargetEvidenceMS: *wire.TargetEvidenceMS}, nil
}

// build 将完整的 wire 阈值转换成值对象。
func (wire scoreThresholdsWire) build() (ScoreThresholds, error) {
	if wire.MinScore == nil || wire.MinMargin == nil {
		return ScoreThresholds{}, fmt.Errorf("%w: thresholds 缺少必填字段", ErrProfileInvalid)
	}
	return ScoreThresholds{MinScore: *wire.MinScore, MinMargin: *wire.MinMargin}, nil
}

// validateMatchingProfile 校验字段完整性与数学安全边界，不提供任何生产阈值默认值。
func validateMatchingProfile(profile MatchingProfile) error {
	if profile.SchemaVersion != matchingProfileSchemaVersion {
		return fmt.Errorf("%w: schema_version 不受支持", ErrProfileInvalid)
	}
	if strings.TrimSpace(profile.ProfileID) == "" || len(profile.ProfileID) > 128 {
		return fmt.Errorf("%w: profile_id 不合法", ErrProfileInvalid)
	}
	if err := validateModelIdentity(profile.Model); err != nil {
		return err
	}
	if profile.Evidence.MinEvidenceMS <= 0 || profile.Evidence.TargetEvidenceMS < profile.Evidence.MinEvidenceMS ||
		profile.Evidence.TargetEvidenceMS > MaxEvidenceDurationMS {
		return fmt.Errorf("%w: evidence 时长不合法", ErrProfileInvalid)
	}
	if err := validateThresholds(profile.Identity); err != nil {
		return err
	}
	if err := validateThresholds(profile.UnknownCluster); err != nil {
		return err
	}
	if !isSafeCalibrationRecord(profile.CalibrationRecord) {
		return fmt.Errorf("%w: calibration_record 不合法", ErrProfileInvalid)
	}
	return nil
}

// validateModelIdentity 校验档案内模型四元组完整且使用小写 SHA-256。
func validateModelIdentity(identity ModelIdentity) error {
	if strings.TrimSpace(identity.ID) == "" || strings.TrimSpace(identity.Version) == "" || identity.Dimension <= 0 {
		return fmt.Errorf("%w: model 身份不完整", ErrProfileInvalid)
	}
	if len(identity.SHA256) != 64 || strings.IndexFunc(identity.SHA256, func(value rune) bool {
		return !(value >= '0' && value <= '9') && !(value >= 'a' && value <= 'f')
	}) >= 0 {
		return fmt.Errorf("%w: model sha256 不合法", ErrProfileInvalid)
	}
	return nil
}

// validateThresholds 只校验 cosine 的数学定义域，不推断校准结论。
func validateThresholds(thresholds ScoreThresholds) error {
	if math.IsNaN(thresholds.MinScore) || math.IsInf(thresholds.MinScore, 0) ||
		thresholds.MinScore < -1 || thresholds.MinScore > 1 {
		return fmt.Errorf("%w: min_score 不合法", ErrProfileInvalid)
	}
	if math.IsNaN(thresholds.MinMargin) || math.IsInf(thresholds.MinMargin, 0) ||
		thresholds.MinMargin < 0 || thresholds.MinMargin > 2 {
		return fmt.Errorf("%w: min_margin 不合法", ErrProfileInvalid)
	}
	return nil
}

// isSafeCalibrationRecord 只接受仓库内相对 Markdown 路径，避免档案引用任意文件。
func isSafeCalibrationRecord(value string) bool {
	cleaned := filepath.Clean(value)
	return value != "" && value == cleaned && !filepath.IsAbs(value) && cleaned != "." &&
		!strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) && filepath.Ext(cleaned) == ".md"
}

// rejectDuplicateJSONKeys 递归遍历 JSON token，拒绝任意层级的重复对象字段。
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanUniqueJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

// scanUniqueJSONValue 读取一个完整 JSON 值，并在对象层维护局部字段集合。
func scanUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		return scanUniqueJSONObject(decoder)
	case '[':
		return scanUniqueJSONArray(decoder)
	default:
		return fmt.Errorf("JSON 起始节点不合法")
	}
}

// scanUniqueJSONObject 扫描一个对象并拒绝当前层重复字段。
func scanUniqueJSONObject(decoder *json.Decoder) error {
	seen := map[string]struct{}{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("JSON 对象字段名不合法")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("JSON 字段 %q 重复", key)
		}
		seen[key] = struct{}{}
		if err := scanUniqueJSONValue(decoder); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

// scanUniqueJSONArray 扫描数组中的每一个完整 JSON 值。
func scanUniqueJSONArray(decoder *json.Decoder) error {
	for decoder.More() {
		if err := scanUniqueJSONValue(decoder); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

// requireJSONEOF 拒绝根值之后的第二个 JSON 值或其他尾随内容。
func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("不允许尾随 JSON 内容")
		}
		return err
	}
	return nil
}
