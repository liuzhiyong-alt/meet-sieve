package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SampleRate 是 Step 3 与实时转写共享的标准 PCM 采样率。
const SampleRate int64 = 16000

// SampleRange 表示以采样点定位的半开音频区间 [Start, End)。
type SampleRange struct {
	Start int64
	End   int64
}

// NewSampleRange 创建非空且非负的半开样本区间。
func NewSampleRange(start int64, end int64) (SampleRange, error) {
	if start < 0 {
		return SampleRange{}, fmt.Errorf("样本区间起点不能为负数")
	}
	if end <= start {
		return SampleRange{}, fmt.Errorf("样本区间必须非空且结束点大于起点")
	}
	return SampleRange{Start: start, End: end}, nil
}

// Duration 返回区间包含的样本数。
func (sampleRange SampleRange) Duration() int64 {
	return sampleRange.End - sampleRange.Start
}

// GapReason 表示实时转写未覆盖一段本地录音的可追溯原因。
type GapReason string

const (
	// GapConnectFailed 表示初次连接未成功建立。
	GapConnectFailed GapReason = "connect_failed"
	// GapDisconnected 表示已建立会话的连接中断。
	GapDisconnected GapReason = "disconnected"
	// GapBackpressure 表示本地 ASR 旁路或处理队列无法安全接受数据。
	GapBackpressure GapReason = "backpressure"
	// GapTailTimeout 表示结束时等待 final 超时。
	GapTailTimeout GapReason = "tail_timeout"
	// GapRecovery 表示崩溃恢复无法确认远端是否已处理的尾部。
	GapRecovery GapReason = "recovery"
	// GapRecordOnly 表示用户选择本场仅录音。
	GapRecordOnly GapReason = "record_only"
)

// IsValid 返回原因是否属于当前 Step 已确认的 gap 原因集合。
func (reason GapReason) IsValid() bool {
	switch reason {
	case GapConnectFailed, GapDisconnected, GapBackpressure, GapTailTimeout, GapRecovery, GapRecordOnly:
		return true
	default:
		return false
	}
}

// BuildGapOriginKey 返回同一会议、原因和样本范围稳定对应的 SHA-256 幂等键。
func BuildGapOriginKey(meetingID string, reason GapReason, sampleRange SampleRange) (string, error) {
	if meetingID == "" {
		return "", fmt.Errorf("会议 ID 不能为空")
	}
	if !reason.IsValid() {
		return "", fmt.Errorf("gap 原因无效：%s", reason)
	}
	if _, err := NewSampleRange(sampleRange.Start, sampleRange.End); err != nil {
		return "", err
	}
	payload := fmt.Sprintf("%s\n%s\n%d\n%d", meetingID, reason, sampleRange.Start, sampleRange.End)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:]), nil
}
