package wails

import (
	"context"
	"fmt"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	transcriptservice "meet-sieve/internal/service/transcript"
)

// ASRSettingsServiceProvider 延迟返回当前工作目录对应的设置服务。
type ASRSettingsServiceProvider func() (*transcriptservice.SettingsService, error)

// ASRRuntimeServiceProvider 延迟返回当前工作目录的 Timeline 与活动转写运行时。
type ASRRuntimeServiceProvider func() (*transcriptservice.TimelineService, *transcriptservice.MeetingRuntime, error)

// CredentialChangeDTO 是凭据字段无歧义的保存动作。
type CredentialChangeDTO struct {
	Action string `json:"action"`
	Value  string `json:"value"`
}

// SaveASRSettingsDTO 是设置页面提交的安全写入契约。
type SaveASRSettingsDTO struct {
	APIKey CredentialChangeDTO `json:"api_key"`
}

// TestASRConnectionDTO 是连接探测使用的内存草稿，不会写入 SQLite。
type TestASRConnectionDTO struct {
	APIKey string `json:"api_key"`
}

// ASRSettingsDTO 是不含任何凭证明文的设置投影。
type ASRSettingsDTO struct {
	APIKeyConfigured      bool   `json:"api_key_configured"`
	APIKeyMask            string `json:"api_key_mask"`
	RequiresAPIKeyUpgrade bool   `json:"requires_api_key_upgrade"`
	UpdatedAt             int64  `json:"updated_at"`
}

// ASRConnectionProbeDTO 明确区分连通性与真实音频验证。
type ASRConnectionProbeDTO struct {
	ConnectionEstablished bool  `json:"connection_established"`
	RealAudioVerified     bool  `json:"real_audio_verified"`
	LatencyMS             int64 `json:"latency_ms"`
}

// ASRTimelineEntryDTO 是持久 final/gap 的判别联合；partial 使用独立事件且 seq 固定为 0。
type ASRTimelineEntryDTO struct {
	Seq          int64  `json:"seq"`
	Kind         string `json:"kind"`
	OccurredAt   int64  `json:"occurred_at"`
	StartSample  int64  `json:"start_sample"`
	EndSample    int64  `json:"end_sample"`
	Text         string `json:"text,omitempty"`
	SpeakerLabel string `json:"speaker_label,omitempty"`
	SessionOrder int    `json:"session_order,omitempty"`
	GapReason    string `json:"gap_reason,omitempty"`
}

// ASRPartialEventDTO 是不持久化的会中临时文本事件，Seq 固定为 0。
type ASRPartialEventDTO struct {
	MeetingID   string `json:"meeting_id"`
	SessionID   string `json:"session_id"`
	Generation  int64  `json:"generation"`
	ResultID    string `json:"result_id"`
	Revision    int64  `json:"revision"`
	Text        string `json:"text"`
	StartSample int64  `json:"start_sample"`
	EndSample   int64  `json:"end_sample"`
}

// ASRPartialClearEventDTO 通知页面清除一个物理 session 或指定 result 的临时文本。
type ASRPartialClearEventDTO struct {
	MeetingID  string `json:"meeting_id"`
	SessionID  string `json:"session_id"`
	Generation int64  `json:"generation"`
	ResultID   string `json:"result_id,omitempty"`
}

// ASRStateEventDTO 是独立实时转写状态事件。
type ASRStateEventDTO struct {
	MeetingID string `json:"meeting_id"`
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

// RawRecordStateDTO 是会议原始记录文件投影的安全状态。
type RawRecordStateDTO struct {
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

// ASRBinding 暴露火山 ASR 设置读取、保存和独立连接探测。
type ASRBinding struct {
	services        ASRSettingsServiceProvider
	runtimeServices ASRRuntimeServiceProvider
	contextProvider ContextProvider
	boundary        *Boundary
}

// NewASRBinding 创建 ASR 设置 binding；构造阶段不访问数据库或网络。
func NewASRBinding(services ASRSettingsServiceProvider, runtimeServices ASRRuntimeServiceProvider, contextProvider ContextProvider, boundary *Boundary) *ASRBinding {
	return &ASRBinding{services: services, runtimeServices: runtimeServices, contextProvider: contextProvider, boundary: boundary}
}

// GetASRSettings 返回当前鉴权方式和凭据掩码状态。
func (binding *ASRBinding) GetASRSettings() Result[ASRSettingsDTO] {
	return Invoke(binding.boundary, "wails.asr.settings.get", func(_ string) (ASRSettingsDTO, error) {
		service, ctx, err := binding.current()
		if err != nil {
			return ASRSettingsDTO{}, err
		}
		view, err := service.GetSettings(ctx)
		return mapASRSettingsDTO(view), err
	})
}

// SaveASRSettings 按字段显式动作保存设置，活动会议期间由服务层阻止修改。
func (binding *ASRBinding) SaveASRSettings(input SaveASRSettingsDTO) Result[ASRSettingsDTO] {
	return Invoke(binding.boundary, "wails.asr.settings.save", func(_ string) (ASRSettingsDTO, error) {
		service, ctx, err := binding.current()
		if err != nil {
			return ASRSettingsDTO{}, err
		}
		view, err := service.SaveSettings(ctx, mapSaveASRSettingsInput(input))
		return mapASRSettingsDTO(view), err
	})
}

// TestASRConnection 使用未保存草稿探测 WebSocket，不发送真实会议音频。
func (binding *ASRBinding) TestASRConnection(input TestASRConnectionDTO) Result[ASRConnectionProbeDTO] {
	return Invoke(binding.boundary, "wails.asr.settings.test", func(_ string) (ASRConnectionProbeDTO, error) {
		service, ctx, err := binding.current()
		if err != nil {
			return ASRConnectionProbeDTO{}, err
		}
		result, err := service.TestConnection(ctx, transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: input.APIKey})
		return mapASRConnectionProbeDTO(result), err
	})
}

// GetASRTimeline 按 seq 游标从 SQLite 恢复 final/gap，默认 100 条、最多 200 条。
func (binding *ASRBinding) GetASRTimeline(meetingID string, afterSeq int64, limit int) Result[[]ASRTimelineEntryDTO] {
	return Invoke(binding.boundary, "wails.asr.timeline", func(_ string) ([]ASRTimelineEntryDTO, error) {
		if binding == nil || binding.runtimeServices == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
			return nil, fmt.Errorf("ASR Timeline 尚未准备")
		}
		timeline, _, err := binding.runtimeServices()
		if err != nil {
			return nil, err
		}
		entries, err := timeline.List(binding.contextProvider(), meetingID, afterSeq, limit)
		if err != nil {
			return nil, err
		}
		return mapASRTimelineDTO(entries), nil
	})
}

// RetryRealtimeASR 对当前活动会议执行一次用户确认后的手动重试。
func (binding *ASRBinding) RetryRealtimeASR(meetingID string) Result[bool] {
	return Invoke(binding.boundary, "wails.asr.retry", func(_ string) (bool, error) {
		if binding == nil || binding.runtimeServices == nil {
			return false, fmt.Errorf("ASR 运行时尚未准备")
		}
		_, runtime, err := binding.runtimeServices()
		if err != nil {
			return false, err
		}
		if err = runtime.Retry(meetingID); err != nil {
			return false, err
		}
		return true, nil
	})
}

// GetRawRecordState 返回原始记录刷新是否仍在等待、写入、最新或失败。
func (binding *ASRBinding) GetRawRecordState(meetingID string) Result[RawRecordStateDTO] {
	return Invoke(binding.boundary, "wails.asr.raw_record.state", func(_ string) (RawRecordStateDTO, error) {
		if binding == nil || binding.runtimeServices == nil || meetingID == "" {
			return RawRecordStateDTO{}, fmt.Errorf("原始记录状态尚未准备")
		}
		_, runtime, err := binding.runtimeServices()
		if err != nil {
			return RawRecordStateDTO{}, err
		}
		state := runtime.RawRecordState(meetingID)
		return RawRecordStateDTO{State: state.State, ErrorCode: state.ErrorCode}, nil
	})
}

// current 返回当前工作目录服务与 Wails 生命周期 context。
func (binding *ASRBinding) current() (*transcriptservice.SettingsService, context.Context, error) {
	if binding == nil || binding.services == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
		return nil, nil, fmt.Errorf("ASR 设置尚未准备")
	}
	service, err := binding.services()
	if err != nil {
		return nil, nil, err
	}
	if service == nil {
		return nil, nil, fmt.Errorf("ASR 设置服务不可用")
	}
	return service, binding.contextProvider(), nil
}

// mapSaveASRSettingsInput 把边界 DTO 转换为领域服务输入。
func mapSaveASRSettingsInput(input SaveASRSettingsDTO) transcriptservice.SaveASRSettingsInput {
	return transcriptservice.SaveASRSettingsInput{APIKey: mapCredentialChange(input.APIKey)}
}

// mapCredentialChange 转换单个凭据动作，不在边界猜测默认行为。
func mapCredentialChange(input CredentialChangeDTO) transcriptservice.CredentialChange {
	return transcriptservice.CredentialChange{Action: transcriptservice.CredentialAction(input.Action), Value: input.Value}
}

// mapASRSettingsDTO 转换只含掩码的安全设置投影。
func mapASRSettingsDTO(view transcriptservice.ASRSettingsView) ASRSettingsDTO {
	return ASRSettingsDTO{
		APIKeyConfigured: view.APIKeyConfigured, APIKeyMask: view.APIKeyMask,
		RequiresAPIKeyUpgrade: view.RequiresAPIKeyUpgrade, UpdatedAt: view.UpdatedAt,
	}
}

// mapASRConnectionProbeDTO 转换连接探测事实。
func mapASRConnectionProbeDTO(result transcriptservice.ConnectionProbeResult) ASRConnectionProbeDTO {
	return ASRConnectionProbeDTO{
		ConnectionEstablished: result.ConnectionEstablished, RealAudioVerified: result.RealAudioVerified,
		LatencyMS: result.LatencyMS,
	}
}

// mapASRTimelineDTO 映射不含内部 UUID 的持久事件联合。
func mapASRTimelineDTO(entries []transcriptservice.TimelineEntry) []ASRTimelineEntryDTO {
	result := make([]ASRTimelineEntryDTO, 0, len(entries))
	for _, entry := range entries {
		result = append(result, ASRTimelineEntryDTO{Seq: entry.Seq, Kind: entry.Kind, OccurredAt: entry.OccurredAt, StartSample: entry.StartSample, EndSample: entry.EndSample, Text: entry.Text, SpeakerLabel: entry.SpeakerLabel, SessionOrder: entry.SessionOrder, GapReason: entry.GapReason})
	}
	return result
}
