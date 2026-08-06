package speaker

import (
	"context"
	"errors"
	"fmt"
	"sort"

	speakerdomain "meet-sieve/internal/domain/speaker"
)

const speakerSampleRate = 16000

// EvidenceState 表示当前 track 证据是否可进入 encoder。
type EvidenceState string

const (
	// EvidenceCollecting 表示证据可读但尚未达到目标，实时阶段继续收集。
	EvidenceCollecting EvidenceState = "collecting"
	// EvidencePending 表示对应音频还没有安全可读来源。
	EvidencePending EvidenceState = "pending"
	// EvidenceReady 表示达到目标或收尾时达到最小证据。
	EvidenceReady EvidenceState = "ready"
	// EvidenceInsufficient 表示 session/会议收尾时仍低于最小证据。
	EvidenceInsufficient EvidenceState = "insufficient"
)

// EvidenceUtterance 是证据构造所需的 final 转写最小投影。
type EvidenceUtterance struct {
	ID             string
	SpeakerTrackID string
	ASRSessionID   string
	SpeakerLabel   string
	FinalSeq       int64
	StartSample    int64
	EndSample      int64
}

// EvidenceItem 记录一条 utterance 是否进入 embedding 及实际使用范围。
type EvidenceItem struct {
	UtteranceID     string
	EvidenceOrder   int
	OverlapRisk     bool
	Included        bool
	ExcludedReason  string
	UsedStartSample int64
	UsedEndSample   int64
}

// EvidenceResult 返回有界 PCM 和可持久化的逐条追溯结果。
type EvidenceResult struct {
	State      EvidenceState
	Samples    []int16
	Items      []EvidenceItem
	DurationMS int64
}

// EvidenceAudioReader 定义按全局采样范围读取安全 PCM 的边界。
type EvidenceAudioReader interface {
	Read(ctx context.Context, meetingID string, startSample int64, endSample int64) ([]int16, error)
}

// EvidenceBuilder 按 final seq 构造单个匿名 track 的确定性证据。
type EvidenceBuilder struct {
	reader EvidenceAudioReader
}

// NewEvidenceBuilder 创建不持有音频文件或数据库事务的证据构造器。
func NewEvidenceBuilder(reader EvidenceAudioReader) *EvidenceBuilder {
	return &EvidenceBuilder{reader: reader}
}

// Build 拼接目标 session/label 证据；finalizing 表示 session 或会议已停止。
func (builder *EvidenceBuilder) Build(
	ctx context.Context,
	meetingID string,
	sessionID string,
	label string,
	trackID string,
	utterances []EvidenceUtterance,
	minEvidenceMS int,
	targetEvidenceMS int,
	finalizing bool,
) (EvidenceResult, error) {
	if err := validateEvidenceRequest(builder, meetingID, sessionID, label, minEvidenceMS, targetEvidenceMS); err != nil {
		return EvidenceResult{}, err
	}
	sorted := sortEvidenceUtterances(utterances)
	targetSamples := int64(targetEvidenceMS) * speakerSampleRate / 1000
	result := EvidenceResult{Samples: make([]int16, 0, targetSamples)}
	for _, utterance := range sorted {
		if utterance.ASRSessionID != sessionID || utterance.SpeakerLabel != label ||
			(trackID != "" && utterance.SpeakerTrackID != trackID) || int64(len(result.Samples)) >= targetSamples {
			continue
		}
		item := buildEvidenceItem(utterance, len(result.Items)+1, sorted)
		if item.OverlapRisk {
			result.Items = append(result.Items, item)
			continue
		}
		remaining := targetSamples - int64(len(result.Samples))
		item.UsedEndSample = minInt64(utterance.EndSample, utterance.StartSample+remaining)
		samples, err := builder.reader.Read(ctx, meetingID, item.UsedStartSample, item.UsedEndSample)
		if errors.Is(err, ErrAudioEvidencePending) {
			result.State = EvidencePending
			return result, nil
		}
		if err != nil {
			return EvidenceResult{}, err
		}
		if int64(len(samples)) != item.UsedEndSample-item.UsedStartSample {
			return EvidenceResult{}, fmt.Errorf("音频读取器返回样本数不一致")
		}
		item.Included = true
		result.Items = append(result.Items, item)
		result.Samples = append(result.Samples, samples...)
	}
	result.DurationMS = int64(len(result.Samples)) * 1000 / speakerSampleRate
	result.State = resolveEvidenceState(result.DurationMS, minEvidenceMS, targetEvidenceMS, finalizing)
	return result, nil
}

// BuildLocalUtterance 为无 provider 标签的单条 final 构造本地音频证据。
// 每条 short final 独立保留，只有达到门槛才会进入匹配或未知聚类，避免伪造稳定身份。
func (builder *EvidenceBuilder) BuildLocalUtterance(
	ctx context.Context,
	meetingID string,
	sessionID string,
	sourceUtteranceID string,
	utterances []EvidenceUtterance,
	minEvidenceMS int,
	targetEvidenceMS int,
	finalizing bool,
) (EvidenceResult, error) {
	if err := validateEvidenceRequest(builder, meetingID, sessionID, "local_utterance", minEvidenceMS, targetEvidenceMS); err != nil {
		return EvidenceResult{}, err
	}
	for _, utterance := range sortEvidenceUtterances(utterances) {
		if utterance.ID != sourceUtteranceID {
			continue
		}
		item := buildLocalEvidenceItem(utterance, sessionID, utterances)
		result := EvidenceResult{Items: []EvidenceItem{item}}
		if item.OverlapRisk {
			result.State = resolveEvidenceState(0, minEvidenceMS, targetEvidenceMS, finalizing)
			return result, nil
		}
		maxSamples := int64(targetEvidenceMS) * speakerSampleRate / 1000
		item.UsedEndSample = minInt64(utterance.EndSample, utterance.StartSample+maxSamples)
		samples, err := builder.reader.Read(ctx, meetingID, item.UsedStartSample, item.UsedEndSample)
		if errors.Is(err, ErrAudioEvidencePending) {
			result.State = EvidencePending
			return result, nil
		}
		if err != nil {
			return EvidenceResult{}, err
		}
		if int64(len(samples)) != item.UsedEndSample-item.UsedStartSample {
			return EvidenceResult{}, fmt.Errorf("音频读取器返回样本数不一致")
		}
		item.Included, result.Items[0] = true, item
		result.Samples = samples
		result.DurationMS = int64(len(samples)) * 1000 / speakerSampleRate
		result.State = resolveEvidenceState(result.DurationMS, minEvidenceMS, targetEvidenceMS, finalizing)
		return result, nil
	}
	return EvidenceResult{}, fmt.Errorf("本地 speaker 源 utterance 不存在")
}

// buildLocalEvidenceItem 拒绝与同一 ASR session 中任意 final 重叠的无标签音频。
func buildLocalEvidenceItem(target EvidenceUtterance, sessionID string, all []EvidenceUtterance) EvidenceItem {
	item := EvidenceItem{UtteranceID: target.ID, EvidenceOrder: 1, UsedStartSample: target.StartSample, UsedEndSample: target.EndSample}
	for _, candidate := range all {
		if candidate.ID == target.ID || candidate.ASRSessionID != sessionID {
			continue
		}
		if target.StartSample < candidate.EndSample && candidate.StartSample < target.EndSample {
			item.OverlapRisk, item.ExcludedReason = true, "overlap_risk"
			return item
		}
	}
	return item
}

// validateEvidenceRequest 校验 builder 依赖和已由 profile 限定的时长关系。
func validateEvidenceRequest(builder *EvidenceBuilder, meetingID string, sessionID string, label string, minMS int, targetMS int) error {
	if builder == nil || builder.reader == nil || meetingID == "" || sessionID == "" || label == "" {
		return fmt.Errorf("证据构造依赖或 track 标识无效")
	}
	if minMS <= 0 || targetMS < minMS || targetMS > speakerdomain.MaxEvidenceDurationMS {
		return fmt.Errorf("证据时长配置无效")
	}
	return nil
}

// sortEvidenceUtterances 按持久 final seq 和 ID 复制排序，不修改调用方切片。
func sortEvidenceUtterances(values []EvidenceUtterance) []EvidenceUtterance {
	result := append([]EvidenceUtterance(nil), values...)
	sort.Slice(result, func(left int, right int) bool {
		if result[left].FinalSeq == result[right].FinalSeq {
			return result[left].ID < result[right].ID
		}
		return result[left].FinalSeq < result[right].FinalSeq
	})
	return result
}

// buildEvidenceItem 标记同 session 不同 label 的正长度重叠范围。
func buildEvidenceItem(target EvidenceUtterance, order int, all []EvidenceUtterance) EvidenceItem {
	item := EvidenceItem{
		UtteranceID: target.ID, EvidenceOrder: order,
		UsedStartSample: target.StartSample, UsedEndSample: target.EndSample,
	}
	for _, candidate := range all {
		if candidate.ID == target.ID || candidate.ASRSessionID != target.ASRSessionID || candidate.SpeakerLabel == target.SpeakerLabel {
			continue
		}
		if target.StartSample < candidate.EndSample && candidate.StartSample < target.EndSample {
			item.OverlapRisk = true
			item.ExcludedReason = "overlap_risk"
			return item
		}
	}
	return item
}

// resolveEvidenceState 按实时与收尾语义判定是否可以进入 encoder。
func resolveEvidenceState(durationMS int64, minMS int, targetMS int, finalizing bool) EvidenceState {
	if durationMS >= int64(targetMS) {
		return EvidenceReady
	}
	if !finalizing {
		return EvidenceCollecting
	}
	if durationMS >= int64(minMS) {
		return EvidenceReady
	}
	return EvidenceInsufficient
}
