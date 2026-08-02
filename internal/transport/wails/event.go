package wails

import "time"

// AppEvent 是 Wails runtime event 的版本化 envelope。
type AppEvent[T any] struct {
	Name       string `json:"name"`
	Version    int    `json:"version"`
	OccurredAt int64  `json:"occurredAt"`
	Sequence   uint64 `json:"sequence,omitempty"`
	Data       T      `json:"data"`
}

// NewEvent 使用统一版本和毫秒时间创建应用事件。
func NewEvent[T any](name string, occurredAt time.Time, sequence uint64, data T) AppEvent[T] {
	return AppEvent[T]{
		Name:       name,
		Version:    1,
		OccurredAt: occurredAt.UnixMilli(),
		Sequence:   sequence,
		Data:       data,
	}
}
