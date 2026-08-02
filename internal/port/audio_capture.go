// Package port 定义业务层需要的外部能力边界。
package port

import (
	"context"
	"errors"
)

var (
	// ErrAudioPermissionDenied 表示操作系统拒绝麦克风采集权限。
	ErrAudioPermissionDenied = errors.New("麦克风权限被拒绝")
	// ErrAudioDeviceUnavailable 表示指定输入设备不存在或当前无法打开。
	ErrAudioDeviceUnavailable = errors.New("麦克风设备不可用")
)

// InputDevice 是不泄漏平台音频 SDK 类型的输入设备描述。
type InputDevice struct {
	ID           string
	Name         string
	IsDefault    bool
	SampleRates  []int
	ChannelCount int
}

// AudioFormat 描述业务统一使用的 PCM 格式。
type AudioFormat struct {
	SampleRate    int
	BitsPerSample int
	Channels      int
}

// AudioFrame 是带采样位置的 PCM 帧。
type AudioFrame struct {
	PCM         []byte
	StartSample int64
}

// AudioStream 表示一次可取消的音频采集会话。
type AudioStream interface {
	ReadFrames(ctx context.Context) (AudioFrame, error)
	Stop(ctx context.Context) error
}

// AudioCapture 提供输入设备枚举、探测和 PCM 采集能力。
// Step 2 已实现会前短录音；Step 3 在同一连续 PCM 契约上增加会议录音编排。
type AudioCapture interface {
	ListInputDevices(ctx context.Context) ([]InputDevice, error)
	TestInputDevice(ctx context.Context, deviceID string) error
	Start(ctx context.Context, deviceID string, format AudioFormat) (AudioStream, error)
}
