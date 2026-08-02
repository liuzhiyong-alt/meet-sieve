// Package clock 提供可替换的系统时间能力。
package clock

import "time"

// Clock 定义业务读取当前时间的稳定边界。
type Clock interface {
	Now() time.Time
}

// System 使用操作系统时间。
type System struct{}

// NewSystem 创建系统时钟。
func NewSystem() *System {
	return &System{}
}

// Now 返回当前系统时间。
func (s *System) Now() time.Time {
	return time.Now()
}

// Fixed 为测试返回固定时间。
type Fixed struct {
	value time.Time
}

// NewFixed 创建固定时钟。
func NewFixed(value time.Time) *Fixed {
	return &Fixed{value: value}
}

// Now 返回创建时配置的固定时间。
func (f *Fixed) Now() time.Time {
	return f.value
}
