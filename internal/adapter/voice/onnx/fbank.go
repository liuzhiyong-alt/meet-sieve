package onnx

import (
	"fmt"
	"math"
)

const (
	fbankSampleRate   = 16000
	fbankFrameSize    = 400
	fbankFrameShift   = 160
	fbankFFTSize      = 512
	fbankBinCount     = 80
	fbankLowFreq      = 20.0
	fbankFloatEpsilon = 1.1920928955078125e-7
)

// extractFBank 按 3D-Speaker infer_sv.py 契约生成展平的 [frames, 80] 特征。
func extractFBank(samples []float32) ([]float32, error) {
	if len(samples) < fbankFrameSize {
		return nil, fmt.Errorf("声纹音频不足一个 25ms 分析窗")
	}
	frameCount := 1 + (len(samples)-fbankFrameSize)/fbankFrameShift
	melBanks := buildMelBanks()
	window := buildPoveyWindow()
	features := make([]float32, frameCount*fbankBinCount)
	for frameIndex := 0; frameIndex < frameCount; frameIndex++ {
		frame := prepareFrame(samples[frameIndex*fbankFrameShift:], window)
		fft(frame)
		for binIndex, weights := range melBanks {
			energy := float32(0)
			for fftIndex, weight := range weights {
				value := frame[fftIndex]
				realPart := float32(real(value))
				imaginaryPart := float32(imag(value))
				energy += weight * (realPart*realPart + imaginaryPart*imaginaryPart)
			}
			if energy < fbankFloatEpsilon {
				energy = fbankFloatEpsilon
			}
			features[frameIndex*fbankBinCount+binIndex] = float32(math.Log(float64(energy)))
		}
	}
	return normalizeFeatureMeans(features, frameCount), nil
}

// prepareFrame 执行逐帧去直流、0.97 预加重、Povey 加窗和补零。
func prepareFrame(samples []float32, window []float32) []complex128 {
	mean := float32(0)
	values := make([]float32, fbankFrameSize)
	for index := range values {
		values[index] = samples[index]
		mean += values[index]
	}
	mean /= fbankFrameSize
	for index := range values {
		values[index] -= mean
	}
	for index := len(values) - 1; index > 0; index-- {
		values[index] -= 0.97 * values[index-1]
	}
	values[0] -= 0.97 * values[0]
	frame := make([]complex128, fbankFFTSize)
	for index, value := range values {
		frame[index] = complex(float64(value*window[index]), 0)
	}
	return frame
}

// buildPoveyWindow 构造 Kaldi 非周期 Hann 窗的 0.85 次幂。
func buildPoveyWindow() []float32 {
	window := make([]float32, fbankFrameSize)
	for index := range window {
		position := float32(2*math.Pi) * float32(index) / float32(fbankFrameSize-1)
		hann := float32(0.5) - float32(0.5)*float32(math.Cos(float64(position)))
		window[index] = float32(math.Pow(float64(hann), 0.85))
	}
	return window
}

// buildMelBanks 构造 Kaldi 20Hz 至 Nyquist 的 80 个三角 mel 滤波器。
func buildMelBanks() [][]float32 {
	melBanks := make([][]float32, fbankBinCount)
	lowMel := melScale(fbankLowFreq)
	highMel := melScale(fbankSampleRate / 2)
	delta := (highMel - lowMel) / float32(fbankBinCount+1)
	for binIndex := range melBanks {
		left := lowMel + float32(binIndex)*delta
		middle := left + delta
		right := middle + delta
		weights := make([]float32, fbankFFTSize/2)
		for fftIndex := range weights {
			mel := melScale(float32(fbankSampleRate*fftIndex) / fbankFFTSize)
			up := (mel - left) / (middle - left)
			down := (right - mel) / (right - middle)
			weight := min(up, down)
			if weight > 0 {
				weights[fftIndex] = weight
			}
		}
		melBanks[binIndex] = weights
	}
	return melBanks
}

// melScale 将 Hz 转为 Kaldi 使用的自然对数 mel 标度。
func melScale(frequency float32) float32 {
	return float32(1127 * math.Log1p(float64(frequency)/700))
}

// normalizeFeatureMeans 对每个 mel 维度执行整段均值归一化。
func normalizeFeatureMeans(features []float32, frameCount int) []float32 {
	means := make([]float32, fbankBinCount)
	for frameIndex := 0; frameIndex < frameCount; frameIndex++ {
		for binIndex := range means {
			means[binIndex] += features[frameIndex*fbankBinCount+binIndex]
		}
	}
	for index := range means {
		means[index] /= float32(frameCount)
	}
	result := make([]float32, len(features))
	for frameIndex := 0; frameIndex < frameCount; frameIndex++ {
		for binIndex := range means {
			offset := frameIndex*fbankBinCount + binIndex
			result[offset] = features[offset] - means[binIndex]
		}
	}
	return result
}

// fft 原地执行 2 的幂长度 Cooley-Tukey FFT。
func fft(values []complex128) {
	for left, right := 1, 0; left < len(values); left++ {
		bit := len(values) >> 1
		for ; right&bit != 0; bit >>= 1 {
			right ^= bit
		}
		right ^= bit
		if left < right {
			values[left], values[right] = values[right], values[left]
		}
	}
	for length := 2; length <= len(values); length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		root := complex(math.Cos(angle), math.Sin(angle))
		for start := 0; start < len(values); start += length {
			factor := complex(1.0, 0)
			for offset := 0; offset < length/2; offset++ {
				even := values[start+offset]
				odd := values[start+offset+length/2] * factor
				values[start+offset] = even + odd
				values[start+offset+length/2] = even - odd
				factor *= root
			}
		}
	}
}
