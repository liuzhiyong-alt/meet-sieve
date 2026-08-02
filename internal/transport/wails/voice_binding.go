package wails

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
	voiceservice "meet-sieve/internal/service/voice"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxImportedWAVBytes = 32 * 1024 * 1024

// VoiceServiceProvider 只在工作目录 ready 后提供当前数据库对应的声纹服务。
type VoiceServiceProvider func() (*voiceservice.VoiceEnrollmentService, *voiceservice.RebuildRunner, error)

// InputDeviceDTO 是录音界面使用的安全设备投影。
type InputDeviceDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	IsDefault    bool   `json:"is_default"`
	ChannelCount int    `json:"channel_count"`
}

// VoiceSampleDTO 是声纹样本列表使用的安全投影。
type VoiceSampleDTO struct {
	ID              string `json:"id"`
	MemberID        string `json:"member_id"`
	DurationMS      int64  `json:"duration_ms"`
	SourceKind      string `json:"source_kind"`
	SourceName      string `json:"source_name"`
	EnvironmentKind string `json:"environment_kind"`
	ProcessingState string `json:"processing_state"`
	QualityState    string `json:"quality_state"`
	QualityCode     string `json:"quality_code"`
	CreatedAt       int64  `json:"created_at"`
}

// VoiceFileSelectionDTO 只向前端返回一次性选择令牌和安全文件名，不暴露系统路径。
type VoiceFileSelectionDTO struct {
	Token    string `json:"token"`
	FileName string `json:"file_name"`
}

// VoiceRecordingStateDTO 是前端轮询的真实录音时长与归一化 PCM 峰值。
type VoiceRecordingStateDTO struct {
	Recording  bool    `json:"recording"`
	Level      float64 `json:"level"`
	DurationMS int64   `json:"duration_ms"`
}

// VoiceBinding 暴露会前录音、WAV 导入、样本管理和向量重建。
type VoiceBinding struct {
	services        VoiceServiceProvider
	capture         port.AudioCapture
	recorder        *voiceservice.Recorder
	contextProvider ContextProvider
	boundary        *Boundary

	mu          sync.Mutex
	recording   bool
	memberID    string
	environment string
	selections  map[string]string
}

// NewVoiceBinding 创建声纹资料 binding，并复用唯一录音器保护设备会话。
func NewVoiceBinding(services VoiceServiceProvider, capture port.AudioCapture, contextProvider ContextProvider, boundary *Boundary) *VoiceBinding {
	return &VoiceBinding{
		services: services, capture: capture, recorder: voiceservice.NewRecorder(capture),
		contextProvider: contextProvider, boundary: boundary, selections: make(map[string]string),
	}
}

// ListInputDevices 返回系统当前报告的麦克风，不打开设备。
func (binding *VoiceBinding) ListInputDevices() Result[[]InputDeviceDTO] {
	return Invoke(binding.boundary, "wails.voice.devices", func(_ string) ([]InputDeviceDTO, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return nil, err
		}
		devices, err := binding.capture.ListInputDevices(ctx)
		if err != nil {
			return nil, err
		}
		result := make([]InputDeviceDTO, 0, len(devices))
		for _, device := range devices {
			result = append(result, InputDeviceDTO{ID: device.ID, Name: device.Name, IsDefault: device.IsDefault, ChannelCount: device.ChannelCount})
		}
		return result, nil
	})
}

// ListVoiceSamples 返回指定活动成员的持久样本状态。
func (binding *VoiceBinding) ListVoiceSamples(memberID string) Result[[]VoiceSampleDTO] {
	return Invoke(binding.boundary, "wails.voice.samples", func(_ string) ([]VoiceSampleDTO, error) {
		service, _, err := binding.services()
		if err != nil {
			return nil, err
		}
		samples, err := service.ListSamples(context.Background(), memberID)
		return mapVoiceSamples(samples), err
	})
}

// ChooseVoiceSample 打开系统 WAV 选择器，仅返回一次性令牌与文件名。
func (binding *VoiceBinding) ChooseVoiceSample() Result[VoiceFileSelectionDTO] {
	return Invoke(binding.boundary, "wails.voice.choose", func(_ string) (VoiceFileSelectionDTO, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return VoiceFileSelectionDTO{}, err
		}
		path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
			Title:   "选择该成员的单人 WAV 录音",
			Filters: []runtime.FileFilter{{DisplayName: "PCM WAV 音频 (*.wav)", Pattern: "*.wav"}},
		})
		if err != nil || path == "" {
			return VoiceFileSelectionDTO{}, err
		}
		token := uuid.NewString()
		binding.mu.Lock()
		binding.selections[token] = path
		binding.mu.Unlock()
		return VoiceFileSelectionDTO{Token: token, FileName: filepath.Base(path)}, nil
	})
}

// ProcessVoiceSample 消费系统选择器生成的一次性令牌，并进入真实录入流水线。
func (binding *VoiceBinding) ProcessVoiceSample(memberID string, environmentKind string, token string) Result[VoiceSampleDTO] {
	return Invoke(binding.boundary, "wails.voice.import", func(_ string) (VoiceSampleDTO, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return VoiceSampleDTO{}, err
		}
		path, ok := binding.takeSelection(token)
		if !ok {
			return VoiceSampleDTO{}, fmt.Errorf("WAV 文件选择已失效，请重新选择")
		}
		wav, err := readBoundedWAV(path)
		if err != nil {
			return VoiceSampleDTO{}, err
		}
		service, _, err := binding.services()
		if err != nil {
			return VoiceSampleDTO{}, err
		}
		sample, err := service.PrepareImported(ctx, memberID, filepath.Base(path), environmentKind, wav)
		if err == nil {
			binding.emitChanged(ctx, memberID)
		}
		return mapVoiceSample(sample), err
	})
}

// StartVoiceRecording 开始唯一会前录音会话，并冻结本次成员与环境归属。
func (binding *VoiceBinding) StartVoiceRecording(memberID string, deviceID string, environmentKind string) Result[bool] {
	return Invoke(binding.boundary, "wails.voice.recording.start", func(_ string) (bool, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return false, err
		}
		service, _, err := binding.services()
		if err != nil {
			return false, err
		}
		if _, err := service.ListSamples(ctx, memberID); err != nil {
			return false, err
		}
		binding.mu.Lock()
		if binding.recording {
			binding.mu.Unlock()
			return false, apperr.Biz(apperr.CodeVoiceRecordingBusy, apperr.WithOp("wails.voice.recording.start"))
		}
		if err := voiceservice.ValidateEnvironmentKind(environmentKind); err != nil {
			binding.mu.Unlock()
			return false, err
		}
		binding.recording, binding.memberID, binding.environment = true, memberID, environmentKind
		binding.mu.Unlock()
		if err := binding.recorder.Start(ctx, deviceID); err != nil {
			binding.clearRecording()
			return false, err
		}
		return true, nil
	})
}

// GetVoiceRecordingState 返回当前录音器的真实采集进度，不传输 PCM 正文。
func (binding *VoiceBinding) GetVoiceRecordingState() Result[VoiceRecordingStateDTO] {
	return Invoke(binding.boundary, "wails.voice.recording.state", func(_ string) (VoiceRecordingStateDTO, error) {
		binding.mu.Lock()
		recording := binding.recording
		binding.mu.Unlock()
		if !recording {
			return VoiceRecordingStateDTO{}, nil
		}
		snapshot := binding.recorder.Snapshot()
		return VoiceRecordingStateDTO{Recording: true, Level: snapshot.Level, DurationMS: snapshot.DurationMS}, nil
	})
}

// StopVoiceRecording 停止录音，并通过与上传完全相同的质量和持久化流程。
func (binding *VoiceBinding) StopVoiceRecording() Result[VoiceSampleDTO] {
	return Invoke(binding.boundary, "wails.voice.recording.stop", func(_ string) (VoiceSampleDTO, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return VoiceSampleDTO{}, err
		}
		memberID, environment, ok := binding.takeRecording()
		if !ok {
			return VoiceSampleDTO{}, apperr.Biz(apperr.CodeVoiceRecordingBusy, apperr.WithOp("wails.voice.recording.stop"))
		}
		recording, err := binding.recorder.Stop(ctx)
		if err != nil {
			return VoiceSampleDTO{}, err
		}
		service, _, err := binding.services()
		if err != nil {
			return VoiceSampleDTO{}, err
		}
		sample, err := service.PrepareRecorded(ctx, memberID, environment, recording)
		if err == nil {
			binding.emitChanged(ctx, memberID)
		}
		return mapVoiceSample(sample), err
	})
}

// CancelVoiceRecording 停止设备并丢弃本次录音，不创建样本。
func (binding *VoiceBinding) CancelVoiceRecording() Result[bool] {
	return Invoke(binding.boundary, "wails.voice.recording.cancel", func(_ string) (bool, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return false, err
		}
		binding.clearRecording()
		err = binding.recorder.Cancel(ctx)
		return err == nil, err
	})
}

// DeleteVoiceSample 删除单个样本及其 embedding，并发布持久状态变化事件。
func (binding *VoiceBinding) DeleteVoiceSample(memberID string, sampleID string) Result[bool] {
	return Invoke(binding.boundary, "wails.voice.sample.delete", func(_ string) (bool, error) {
		service, _, err := binding.services()
		if err != nil {
			return false, err
		}
		err = service.DeleteSample(context.Background(), memberID, sampleID)
		if err == nil {
			if ctx, contextErr := binding.currentContext(); contextErr == nil {
				binding.emitChanged(ctx, memberID)
			}
		}
		return err == nil, err
	})
}

// DeleteAllVoiceSamples 删除成员当前全部声纹；失败时返回错误而不报告完整成功。
func (binding *VoiceBinding) DeleteAllVoiceSamples(memberID string) Result[bool] {
	return Invoke(binding.boundary, "wails.voice.samples.delete_all", func(_ string) (bool, error) {
		service, _, err := binding.services()
		if err != nil {
			return false, err
		}
		err = service.DeleteAllSamples(context.Background(), memberID)
		return err == nil, err
	})
}

// RebuildVoiceEmbeddings 继续执行当前模型缺失向量的可恢复重建。
func (binding *VoiceBinding) RebuildVoiceEmbeddings() Result[voiceservice.RebuildProgress] {
	return Invoke(binding.boundary, "wails.voice.rebuild", func(_ string) (voiceservice.RebuildProgress, error) {
		_, runner, err := binding.services()
		if err != nil {
			return voiceservice.RebuildProgress{}, err
		}
		return runner.Run(context.Background())
	})
}

// currentContext 返回仍有效的 Wails 生命周期 context。
func (binding *VoiceBinding) currentContext() (context.Context, error) {
	if binding == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
		return nil, fmt.Errorf("声纹资料功能尚未准备")
	}
	return binding.contextProvider(), nil
}

// takeRecording 原子取得并清除当前录音归属，防止重复 Stop 生成两份样本。
func (binding *VoiceBinding) takeRecording() (string, string, bool) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if !binding.recording {
		return "", "", false
	}
	memberID, environment := binding.memberID, binding.environment
	binding.recording, binding.memberID, binding.environment = false, "", ""
	return memberID, environment, true
}

// clearRecording 清除尚未进入处理流水线的录音归属。
func (binding *VoiceBinding) clearRecording() {
	binding.mu.Lock()
	binding.recording, binding.memberID, binding.environment = false, "", ""
	binding.mu.Unlock()
}

// takeSelection 原子消费系统文件选择结果，防止重复处理或前端伪造路径。
func (binding *VoiceBinding) takeSelection(token string) (string, bool) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	path, ok := binding.selections[token]
	if ok {
		delete(binding.selections, token)
	}
	return path, ok
}

// emitChanged 发布低频失效事件；页面仍通过 query 恢复完整状态。
func (binding *VoiceBinding) emitChanged(ctx context.Context, memberID string) {
	runtime.EventsEmit(ctx, "voice.sample.completed", map[string]string{"member_id": memberID})
	runtime.EventsEmit(ctx, "people.projection.changed", map[string]string{"member_id": memberID})
}

// readBoundedWAV 在分配内存前限制导入文件大小，格式与时长仍由业务解析器判断。
func readBoundedWAV(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxImportedWAVBytes {
		return nil, fmt.Errorf("WAV 文件过大或不是普通文件")
	}
	return os.ReadFile(path)
}

// mapVoiceSamples 映射列表且不暴露文件相对路径与质量指标正文。
func mapVoiceSamples(samples []voiceservice.VoiceSample) []VoiceSampleDTO {
	result := make([]VoiceSampleDTO, 0, len(samples))
	for _, sample := range samples {
		result = append(result, mapVoiceSample(sample))
	}
	return result
}

// mapVoiceSample 映射单个安全样本投影。
func mapVoiceSample(sample voiceservice.VoiceSample) VoiceSampleDTO {
	return VoiceSampleDTO{
		ID: sample.ID, MemberID: sample.MemberID, DurationMS: sample.DurationMS, SourceKind: sample.SourceKind,
		SourceName: sample.SourceName, EnvironmentKind: sample.EnvironmentKind, ProcessingState: sample.ProcessingState,
		QualityState: sample.QualityState, QualityCode: sample.QualityCode, CreatedAt: sample.CreatedAt,
	}
}
