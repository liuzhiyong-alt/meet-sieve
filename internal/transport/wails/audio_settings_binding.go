package wails

import (
	"context"
	"time"

	audioservice "meet-sieve/internal/service/audio"
)

// AudioSettingsDTO 是录音分类独立设置契约。
type AudioSettingsDTO struct {
	DefaultMicrophoneID string           `json:"default_microphone_id"`
	Revision            int64            `json:"revision"`
	Devices             []InputDeviceDTO `json:"devices"`
}

// AudioSettingsServiceProvider 返回当前工作目录录音设置服务。
type AudioSettingsServiceProvider func() (*audioservice.SettingsService, error)

// AudioSettingsBinding 暴露录音分类独立加载、保存和真实测试。
type AudioSettingsBinding struct {
	service  AudioSettingsServiceProvider
	boundary *Boundary
}

// NewAudioSettingsBinding 创建录音设置 Binding。
func NewAudioSettingsBinding(service AudioSettingsServiceProvider, boundary *Boundary) *AudioSettingsBinding {
	return &AudioSettingsBinding{service: service, boundary: boundary}
}

// GetAudioSettings 返回保存值和实时设备列表。
func (binding *AudioSettingsBinding) GetAudioSettings() Result[AudioSettingsDTO] {
	return Invoke(binding.boundary, "wails.audio_settings.get", func(string) (AudioSettingsDTO, error) {
		service, err := binding.service()
		if err != nil {
			return AudioSettingsDTO{}, err
		}
		settings, err := service.Get(context.Background())
		return mapAudioSettingsDTO(settings), err
	})
}

// SaveAudioSettings 独立保存默认麦克风。
func (binding *AudioSettingsBinding) SaveAudioSettings(deviceID string) Result[AudioSettingsDTO] {
	return Invoke(binding.boundary, "wails.audio_settings.save", func(string) (AudioSettingsDTO, error) {
		service, err := binding.service()
		if err != nil {
			return AudioSettingsDTO{}, err
		}
		settings, err := service.Save(context.Background(), deviceID)
		return mapAudioSettingsDTO(settings), err
	})
}

// TestAudioDevice 执行最长五秒的真实设备探测，不保存设置。
func (binding *AudioSettingsBinding) TestAudioDevice(deviceID string) Result[bool] {
	return Invoke(binding.boundary, "wails.audio_settings.test", func(string) (bool, error) {
		service, err := binding.service()
		if err != nil {
			return false, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = service.TestDevice(ctx, deviceID)
		return err == nil, err
	})
}

// mapAudioSettingsDTO 转换设备列表而不暴露平台 SDK 类型。
func mapAudioSettingsDTO(settings audioservice.Settings) AudioSettingsDTO {
	devices := make([]InputDeviceDTO, 0, len(settings.Devices))
	for _, device := range settings.Devices {
		devices = append(devices, InputDeviceDTO{ID: device.ID, Name: device.Name, IsDefault: device.IsDefault, ChannelCount: device.ChannelCount})
	}
	return AudioSettingsDTO{DefaultMicrophoneID: settings.DefaultMicrophoneID, Revision: settings.Revision, Devices: devices}
}
