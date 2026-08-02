package voice

import (
	"context"
	"math"
)

const resampleFilterRadius = 12

// resamplePCM16 使用固定窗口带限 sinc，将单声道 PCM 确定性转换到目标采样率。
func resamplePCM16(ctx context.Context, input []int16, sourceRate int, targetRate int) ([]int16, error) {
	if sourceRate == targetRate {
		return input, nil
	}
	outputLength := (len(input)*targetRate + sourceRate/2) / sourceRate
	output := make([]int16, outputLength)
	cutoff := math.Min(1, float64(targetRate)/float64(sourceRate))
	for outputIndex := range output {
		if outputIndex%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		sourcePosition := float64(outputIndex) * float64(sourceRate) / float64(targetRate)
		output[outputIndex] = interpolateSample(input, sourcePosition, cutoff)
	}
	return output, nil
}

// interpolateSample 使用 Hann 窗截断 sinc，并在文件边界重新归一化权重。
func interpolateSample(input []int16, position float64, cutoff float64) int16 {
	center := int(math.Floor(position))
	weightedSum := 0.0
	weightSum := 0.0
	for sampleIndex := center - resampleFilterRadius + 1; sampleIndex <= center+resampleFilterRadius; sampleIndex++ {
		if sampleIndex < 0 || sampleIndex >= len(input) {
			continue
		}
		distance := position - float64(sampleIndex)
		weight := cutoff * sinc(distance*cutoff) * hannWindow(distance)
		weightedSum += float64(input[sampleIndex]) * weight
		weightSum += weight
	}
	if math.Abs(weightSum) < 1e-12 {
		return 0
	}
	return clampPCM16(math.Round(weightedSum / weightSum))
}

// sinc 返回归一化 sinc 函数值。
func sinc(value float64) float64 {
	if math.Abs(value) < 1e-12 {
		return 1
	}
	angle := math.Pi * value
	return math.Sin(angle) / angle
}

// hannWindow 返回固定半径的 Hann 窗权重。
func hannWindow(distance float64) float64 {
	if math.Abs(distance) >= resampleFilterRadius {
		return 0
	}
	return 0.5 * (1 + math.Cos(math.Pi*distance/resampleFilterRadius))
}

// clampPCM16 将舍入结果限制到 16-bit PCM 有符号范围。
func clampPCM16(value float64) int16 {
	if value > math.MaxInt16 {
		return math.MaxInt16
	}
	if value < math.MinInt16 {
		return math.MinInt16
	}
	return int16(value)
}
