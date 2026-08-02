// Package identity 提供可替换的唯一 ID 生成能力。
package identity

import (
	"sync"

	"github.com/google/uuid"
)

// Generator 定义业务生成唯一 ID 的稳定边界。
type Generator interface {
	New() string
}

// UUIDGenerator 使用 UUID v4 生成生产 ID。
type UUIDGenerator struct{}

// NewUUIDGenerator 创建 UUID 生成器。
func NewUUIDGenerator() *UUIDGenerator {
	return &UUIDGenerator{}
}

// New 返回新的 UUID 字符串。
func (g *UUIDGenerator) New() string {
	return uuid.NewString()
}

// FixedGenerator 为测试按顺序返回预设 ID，并支持并发调用。
type FixedGenerator struct {
	mu     sync.Mutex
	values []string
	index  int
}

// NewFixedGenerator 创建固定 ID 生成器。
func NewFixedGenerator(values ...string) *FixedGenerator {
	copied := append([]string(nil), values...)
	return &FixedGenerator{values: copied}
}

// New 返回下一个预设 ID；序列耗尽后返回空字符串。
func (g *FixedGenerator) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.index >= len(g.values) {
		return ""
	}
	value := g.values[g.index]
	g.index++
	return value
}
