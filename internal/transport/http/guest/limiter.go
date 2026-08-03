package guest

import (
	"sync"
	"time"
)

const maxLimiterKeys = 4096

type limitWindow struct {
	startedAt time.Time
	count     int
}

// Limiter 使用有界固定窗口限制 Guest API，并由 generation 键自然隔离会议。
type Limiter struct {
	mu      sync.Mutex
	windows map[string]limitWindow
}

// NewLimiter 创建最多保留 4096 个活动键的限流器。
func NewLimiter() *Limiter {
	return &Limiter{windows: make(map[string]limitWindow)}
}

// Allow 在固定窗口内不超过 limit 时增加计数。
func (limiter *Limiter) Allow(key string, limit int, window time.Duration, now time.Time) bool {
	if limiter == nil || key == "" || limit <= 0 || window <= 0 {
		return false
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	current, exists := limiter.windows[key]
	if !exists || now.Sub(current.startedAt) >= window {
		if !exists && len(limiter.windows) >= maxLimiterKeys {
			limiter.removeExpired(now, window)
			if len(limiter.windows) >= maxLimiterKeys {
				return false
			}
		}
		limiter.windows[key] = limitWindow{startedAt: now, count: 1}
		return true
	}
	if current.count >= limit {
		return false
	}
	current.count++
	limiter.windows[key] = current
	return true
}

// Reset 在 LAN generation 停止时清理所有内存限流状态。
func (limiter *Limiter) Reset() {
	if limiter == nil {
		return
	}
	limiter.mu.Lock()
	limiter.windows = make(map[string]limitWindow)
	limiter.mu.Unlock()
}

// removeExpired 在容量满时清理过期窗口，不启动后台 goroutine。
func (limiter *Limiter) removeExpired(now time.Time, window time.Duration) {
	for key, current := range limiter.windows {
		if now.Sub(current.startedAt) >= window {
			delete(limiter.windows, key)
		}
	}
}

// Presence 保存最近 45 秒内有成功请求的 session。
type Presence struct {
	mu       sync.Mutex
	lastSeen map[string]time.Time
}

// NewPresence 创建有界在线会话表。
func NewPresence() *Presence { return &Presence{lastSeen: make(map[string]time.Time)} }

// Mark 记录认证成功的 session 最后请求时间。
func (presence *Presence) Mark(sessionID string, now time.Time) {
	if presence == nil || sessionID == "" {
		return
	}
	presence.mu.Lock()
	if _, exists := presence.lastSeen[sessionID]; exists || len(presence.lastSeen) < maxLimiterKeys {
		presence.lastSeen[sessionID] = now
	}
	presence.mu.Unlock()
}

// Count 返回 45 秒内活动 session 数，同时删除过期项。
func (presence *Presence) Count(now time.Time) int {
	if presence == nil {
		return 0
	}
	presence.mu.Lock()
	defer presence.mu.Unlock()
	for sessionID, lastSeen := range presence.lastSeen {
		if now.Sub(lastSeen) > 45*time.Second {
			delete(presence.lastSeen, sessionID)
		}
	}
	return len(presence.lastSeen)
}

// Reset 清理已停止 generation 的在线状态。
func (presence *Presence) Reset() {
	if presence == nil {
		return
	}
	presence.mu.Lock()
	presence.lastSeen = make(map[string]time.Time)
	presence.mu.Unlock()
}
