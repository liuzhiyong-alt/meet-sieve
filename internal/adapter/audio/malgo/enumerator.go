// Package malgo 封装 miniaudio 的设备枚举能力。
package malgo

import (
	"context"
	"fmt"
	"sync"

	"meet-sieve/internal/port"

	malgoSDK "github.com/gen2brain/malgo"
)

// Enumerator 使用 malgo 枚举本机输入设备。
type Enumerator struct {
	mu     sync.Mutex
	active bool
}

// NewEnumerator 创建音频设备枚举适配器。
func NewEnumerator() *Enumerator {
	return &Enumerator{}
}

// ListInputDevices 返回真实系统报告的输入设备；无设备时返回空列表。
func (e *Enumerator) ListInputDevices(ctx context.Context) ([]port.InputDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	deviceContext, err := malgoSDK.InitContext(nil, malgoSDK.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化音频上下文失败: %w", err)
	}
	defer func() {
		_ = deviceContext.Uninit()
		deviceContext.Free()
	}()

	devices, err := deviceContext.Devices(malgoSDK.Capture)
	if err != nil {
		return nil, fmt.Errorf("枚举输入设备失败: %w", err)
	}
	return mapDevices(devices), nil
}

// mapDevices 将 malgo 设备结构转换为不泄漏框架类型的 Port DTO。
func mapDevices(devices []malgoSDK.DeviceInfo) []port.InputDevice {
	result := make([]port.InputDevice, 0, len(devices))
	for index, device := range devices {
		sampleRates := make([]int, 0, len(device.Formats))
		channels := 0
		for _, format := range device.Formats {
			sampleRates = appendUniqueRate(sampleRates, int(format.SampleRate))
			if int(format.Channels) > channels {
				channels = int(format.Channels)
			}
		}
		result = append(result, port.InputDevice{
			ID:           device.ID.String(),
			Name:         device.Name(),
			IsDefault:    device.IsDefault != 0,
			SampleRates:  sampleRates,
			ChannelCount: channels,
		})
		_ = index
	}
	return result
}

// appendUniqueRate 保留设备报告顺序并去除重复采样率。
func appendUniqueRate(rates []int, candidate int) []int {
	for _, rate := range rates {
		if rate == candidate {
			return rates
		}
	}
	return append(rates, candidate)
}

// TestInputDevice 短暂打开指定设备，验证权限和设备可用性。
func (e *Enumerator) TestInputDevice(ctx context.Context, deviceID string) error {
	stream, err := e.Start(ctx, deviceID, port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1})
	if err != nil {
		return err
	}
	return stream.Stop(ctx)
}

// Start 打开单个 16 kHz、16-bit、单声道 PCM 采集会话。
func (e *Enumerator) Start(ctx context.Context, deviceID string, format port.AudioFormat) (port.AudioStream, error) {
	if err := validateCaptureRequest(ctx, deviceID, format); err != nil {
		return nil, err
	}
	if err := ensureCapturePermission(); err != nil {
		return nil, err
	}
	if !e.reserveSession() {
		return nil, fmt.Errorf("已有音频采集会话")
	}
	stream, err := e.startReserved(deviceID, format)
	if err != nil {
		e.releaseSession()
		return nil, err
	}
	return stream, nil
}

// validateCaptureRequest 拒绝不能由声纹录制链路消费的请求。
func validateCaptureRequest(ctx context.Context, deviceID string, format port.AudioFormat) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deviceID == "" {
		return fmt.Errorf("输入设备 ID 不能为空")
	}
	if format.SampleRate != 16000 || format.BitsPerSample != 16 || format.Channels != 1 {
		return fmt.Errorf("录音格式必须为 16 kHz、16-bit、单声道")
	}
	return nil
}

// reserveSession 原子占用当前 adapter 的唯一采集会话。
func (e *Enumerator) reserveSession() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active {
		return false
	}
	e.active = true
	return true
}

// releaseSession 释放采集会话占用。
func (e *Enumerator) releaseSession() {
	e.mu.Lock()
	e.active = false
	e.mu.Unlock()
}

// startReserved 初始化 malgo context、设备和回调队列。
func (e *Enumerator) startReserved(deviceID string, format port.AudioFormat) (*captureStream, error) {
	deviceContext, err := malgoSDK.InitContext(nil, malgoSDK.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化音频上下文失败: %w", err)
	}
	selected, err := findCaptureDevice(deviceContext, deviceID)
	if err != nil {
		_ = deviceContext.Uninit()
		deviceContext.Free()
		return nil, err
	}
	stream := newCaptureStream(deviceContext, e.releaseSession)
	config := malgoSDK.DefaultDeviceConfig(malgoSDK.Capture)
	deviceIDPointer := selected.ID.Pointer()
	defer freeDeviceIDPointer(deviceIDPointer)
	config.Capture.DeviceID = deviceIDPointer
	config.Capture.Format = malgoSDK.FormatS16
	config.Capture.Channels = uint32(format.Channels)
	config.SampleRate = uint32(format.SampleRate)
	device, err := malgoSDK.InitDevice(deviceContext.Context, config, stream.callbacks())
	if err != nil {
		stream.releaseContext()
		return nil, fmt.Errorf("打开输入设备失败: %w", classifyCaptureError(err))
	}
	stream.device = device
	if err := device.Start(); err != nil {
		stream.stopResources()
		return nil, fmt.Errorf("启动输入设备失败: %w", classifyCaptureError(err))
	}
	return stream, nil
}

// findCaptureDevice 按枚举返回的稳定 ID 选择设备。
func findCaptureDevice(deviceContext *malgoSDK.AllocatedContext, deviceID string) (malgoSDK.DeviceInfo, error) {
	devices, err := deviceContext.Devices(malgoSDK.Capture)
	if err != nil {
		return malgoSDK.DeviceInfo{}, fmt.Errorf("枚举输入设备失败: %w", classifyCaptureError(err))
	}
	for _, device := range devices {
		if device.ID.String() == deviceID {
			return device, nil
		}
	}
	return malgoSDK.DeviceInfo{}, fmt.Errorf("输入设备不存在: %w", port.ErrAudioDeviceUnavailable)
}
