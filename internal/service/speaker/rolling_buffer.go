package speaker

import (
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrRollingDiscontinuity 表示写入起点与已接收样本尾不连续，禁止补零掩盖缺口。
	ErrRollingDiscontinuity = errors.New("rolling buffer 样本不连续")
	// ErrRollingClosed 表示录音链路已经关闭 rolling buffer。
	ErrRollingClosed = errors.New("rolling buffer 已关闭")
)

// RollingBuffer 按全局采样点保存固定容量的最近单声道 PCM16 样本。
type RollingBuffer struct {
	mu          sync.RWMutex
	samples     []int16
	startSample int64
	endSample   int64
	initialized bool
	closed      bool
}

// NewRollingBuffer 创建固定样本容量的同步 ring；调用方按 16000 样本/秒传入 120 秒容量。
func NewRollingBuffer(capacitySamples int) (*RollingBuffer, error) {
	if capacitySamples <= 0 {
		return nil, fmt.Errorf("rolling buffer 容量必须为正数")
	}
	return &RollingBuffer{samples: make([]int16, capacitySamples)}, nil
}

// Write 同步复制一段连续 PCM，不启动 goroutine，也不等待后台消费者。
func (buffer *RollingBuffer) Write(startSample int64, samples []int16) error {
	if buffer == nil || startSample < 0 {
		return fmt.Errorf("rolling buffer 写入范围无效")
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.closed {
		return ErrRollingClosed
	}
	if buffer.initialized && startSample != buffer.endSample {
		return ErrRollingDiscontinuity
	}
	if len(samples) == 0 {
		return nil
	}
	if int64(len(samples)) > int64(^uint64(0)>>1)-startSample {
		return fmt.Errorf("rolling buffer 样本范围溢出")
	}
	for index, sample := range samples {
		position := (startSample + int64(index)) % int64(len(buffer.samples))
		buffer.samples[position] = sample
	}
	buffer.endSample = startSample + int64(len(samples))
	buffer.startSample = buffer.endSample - minInt64(buffer.endSample, int64(len(buffer.samples)))
	buffer.initialized = true
	return nil
}

// Reset 为下一场从零开始的连续录音清空范围状态；固定容量内存会被后续样本覆盖。
func (buffer *RollingBuffer) Reset() {
	if buffer == nil {
		return
	}
	buffer.mu.Lock()
	buffer.startSample = 0
	buffer.endSample = 0
	buffer.initialized = false
	buffer.closed = false
	buffer.mu.Unlock()
}

// Read 返回全局半开区间的独立副本；任一样本已覆盖或尚未到达时返回 false。
func (buffer *RollingBuffer) Read(startSample int64, endSample int64) ([]int16, bool) {
	if buffer == nil || startSample < 0 || endSample <= startSample {
		return nil, false
	}
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	if !buffer.initialized || startSample < buffer.startSample || endSample > buffer.endSample {
		return nil, false
	}
	result := make([]int16, endSample-startSample)
	for index := range result {
		position := (startSample + int64(index)) % int64(len(buffer.samples))
		result[index] = buffer.samples[position]
	}
	return result, true
}

// Close 禁止后续录音写入，但保留当前证据供后台任务完成读取。
func (buffer *RollingBuffer) Close() {
	if buffer == nil {
		return
	}
	buffer.mu.Lock()
	buffer.closed = true
	buffer.mu.Unlock()
}

// minInt64 返回两个非负整数中的较小值。
func minInt64(left int64, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
