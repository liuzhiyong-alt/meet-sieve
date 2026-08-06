package agent

import (
	"encoding/binary"
	"math"

	"meet-sieve/internal/port"
)

const (
	minimumVoiceRMS  = 0.012
	minimumVoicePeak = 0.035
)

// adaptiveVoiceDetector 使用自适应噪声底和双重能量门限判断本地 PCM 是否包含有效语音。
// 它只负责端点静音计时，不参与声纹识别，也不会保存原始音频。
type adaptiveVoiceDetector struct {
	noiseFloor float64
}

// observe 返回当前帧是否包含足以刷新端点计时的语音活动。
func (detector *adaptiveVoiceDetector) observe(frame port.AudioFrame) bool {
	rms, peak, valid := pcmEnergy(frame.PCM)
	if !valid {
		return false
	}
	if detector.noiseFloor == 0 {
		detector.noiseFloor = math.Min(rms, minimumVoiceRMS/2)
	}
	threshold := math.Max(minimumVoiceRMS, detector.noiseFloor*3)
	voiced := rms >= threshold && peak >= minimumVoicePeak
	if !voiced {
		// 只用非语音帧缓慢追踪环境噪声，避免发言本身抬高门限。
		detector.noiseFloor = detector.noiseFloor*0.98 + rms*0.02
	}
	return voiced
}

// pcmEnergy 计算 16-bit little-endian 单声道 PCM 的归一化 RMS 与峰值。
func pcmEnergy(pcm []byte) (rms float64, peak float64, valid bool) {
	if len(pcm) < 2 || len(pcm)%2 != 0 {
		return 0, 0, false
	}
	var sum float64
	for index := 0; index < len(pcm); index += 2 {
		value := float64(int16(binary.LittleEndian.Uint16(pcm[index:index+2]))) / 32768
		absolute := math.Abs(value)
		if absolute > peak {
			peak = absolute
		}
		sum += value * value
	}
	samples := float64(len(pcm) / 2)
	return math.Sqrt(sum / samples), peak, true
}
