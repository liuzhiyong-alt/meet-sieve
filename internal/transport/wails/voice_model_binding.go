package wails

import (
	"context"
	"fmt"

	application "meet-sieve/internal/app"
	voiceservice "meet-sieve/internal/service/voice"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// VoiceModelDTO 是设置页展示的官方模型安全状态。
type VoiceModelDTO struct {
	State        string `json:"state"`
	Usable       bool   `json:"usable"`
	ModelID      string `json:"modelId"`
	ModelName    string `json:"modelName"`
	ModelVersion string `json:"modelVersion"`
	ModelSize    int64  `json:"modelSize"`
	Location     string `json:"location"`
}

// VoiceModelProxyPortProvider 返回当前工作目录保存的本机 HTTP(S) 代理端口。
type VoiceModelProxyPortProvider func(context.Context) (int, error)

// VoiceModelBinding 暴露官方模型状态、下载与同包离线导入。
type VoiceModelBinding struct {
	module          *application.VoiceModule
	contextProvider ContextProvider
	boundary        *Boundary
	afterActivate   func(context.Context) error
	proxyPort       VoiceModelProxyPortProvider
}

// SetAfterActivate 登记模型激活后恢复 pending 样本与重建向量的回调。
func (binding *VoiceModelBinding) SetAfterActivate(callback func(context.Context) error) {
	if binding != nil {
		binding.afterActivate = callback
	}
}

// NewVoiceModelBinding 创建声纹模型设置 binding。
func NewVoiceModelBinding(module *application.VoiceModule, contextProvider ContextProvider, boundary *Boundary, proxyPort VoiceModelProxyPortProvider) *VoiceModelBinding {
	return &VoiceModelBinding{module: module, contextProvider: contextProvider, boundary: boundary, proxyPort: proxyPort}
}

// GetVoiceModelState 返回当前模型与运行时的只读状态。
func (binding *VoiceModelBinding) GetVoiceModelState() Result[VoiceModelDTO] {
	return Invoke(binding.boundary, "wails.voice_model.state", func(_ string) (VoiceModelDTO, error) {
		if binding == nil || binding.module == nil {
			return VoiceModelDTO{}, fmt.Errorf("声纹模型模块不可用")
		}
		return mapVoiceModelDTO(binding.module.Status()), nil
	})
}

// DownloadOfficialVoiceModel 下载固定 GitHub Release 包并立即激活模型。
func (binding *VoiceModelBinding) DownloadOfficialVoiceModel() Result[VoiceModelDTO] {
	return Invoke(binding.boundary, "wails.voice_model.download", func(_ string) (VoiceModelDTO, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return VoiceModelDTO{}, err
		}
		proxyPort, err := binding.currentProxyPort(ctx)
		if err != nil {
			return VoiceModelDTO{}, err
		}
		status, err := binding.module.Download(ctx, proxyPort)
		if err == nil && binding.afterActivate != nil {
			err = binding.afterActivate(ctx)
		}
		return mapVoiceModelDTO(status), err
	})
}

// ImportOfflineVoiceModel 打开系统文件选择器并安装同一官方 ZIP；取消不视为错误。
func (binding *VoiceModelBinding) ImportOfflineVoiceModel() Result[VoiceModelDTO] {
	return Invoke(binding.boundary, "wails.voice_model.import", func(_ string) (VoiceModelDTO, error) {
		ctx, err := binding.currentContext()
		if err != nil {
			return VoiceModelDTO{}, err
		}
		path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
			Title:   "导入 MeetSieve 官方声纹模型包",
			Filters: []runtime.FileFilter{{DisplayName: "MeetSieve 声纹模型包 (*.zip)", Pattern: "*.zip"}},
		})
		if err != nil {
			return VoiceModelDTO{}, err
		}
		if path == "" {
			return mapVoiceModelDTO(binding.module.Status()), nil
		}
		status, err := binding.module.Import(ctx, path)
		if err == nil && binding.afterActivate != nil {
			err = binding.afterActivate(ctx)
		}
		return mapVoiceModelDTO(status), err
	})
}

// currentContext 返回仍有效的 Wails 生命周期 context。
func (binding *VoiceModelBinding) currentContext() (context.Context, error) {
	if binding == nil || binding.module == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
		return nil, fmt.Errorf("声纹模型设置尚未准备")
	}
	return binding.contextProvider(), nil
}

// currentProxyPort 缺少工作目录配置时保持直连，避免影响离线导入和状态读取。
func (binding *VoiceModelBinding) currentProxyPort(ctx context.Context) (int, error) {
	if binding == nil || binding.proxyPort == nil {
		return 0, nil
	}
	return binding.proxyPort(ctx)
}

// mapVoiceModelDTO 将内部状态映射为设计稿登记的四类展示状态。
func mapVoiceModelDTO(status application.VoiceModuleStatus) VoiceModelDTO {
	state := string(status.Model.State)
	if status.Initializing {
		state = "initializing"
	} else if status.Model.State == voiceservice.ModelStateReady && !status.Usable {
		state = "verification_failed"
	}
	return VoiceModelDTO{
		State: state, Usable: status.Usable, ModelID: status.Model.ModelID,
		ModelName: "CAM++ 中文通用", ModelVersion: status.Model.ModelVersion,
		ModelSize: status.Model.ModelSize, Location: "本机应用数据目录",
	}
}
