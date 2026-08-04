package guest

import (
	"sync"
)

// TimelineNotification 是 WebSocket 发送的轻量失效通知，不携带会议正文。
type TimelineNotification struct {
	Type      string `json:"type"`
	MeetingID string `json:"meeting_id"`
	LatestSeq int64  `json:"latest_seq"`
	Reason    string `json:"reason"`
}

// Hub 按会议维护进程内订阅者；慢客户端只保留最新一条通知。
type Hub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan TimelineNotification]struct{}
}

// NewHub 创建不持有会议正文的 WebSocket 通知中心。
func NewHub() *Hub {
	return &Hub{subscribers: make(map[string]map[chan TimelineNotification]struct{})}
}

// Subscribe 订阅指定会议，并返回幂等取消函数。
func (hub *Hub) Subscribe(meetingID string) (<-chan TimelineNotification, func()) {
	channel := make(chan TimelineNotification, 1)
	if hub == nil || meetingID == "" {
		close(channel)
		return channel, func() {}
	}
	hub.mu.Lock()
	if hub.subscribers[meetingID] == nil {
		hub.subscribers[meetingID] = make(map[chan TimelineNotification]struct{})
	}
	hub.subscribers[meetingID][channel] = struct{}{}
	hub.mu.Unlock()
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			hub.mu.Lock()
			delete(hub.subscribers[meetingID], channel)
			if len(hub.subscribers[meetingID]) == 0 {
				delete(hub.subscribers, meetingID)
			}
			close(channel)
			hub.mu.Unlock()
		})
	}
}

// Publish 非阻塞广播；若客户端尚未消费旧通知，则用更新的 seq 覆盖。
func (hub *Hub) Publish(meetingID string, latestSeq int64, reason string) {
	if hub == nil || meetingID == "" {
		return
	}
	notification := TimelineNotification{Type: "timeline.changed", MeetingID: meetingID, LatestSeq: latestSeq, Reason: reason}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for channel := range hub.subscribers[meetingID] {
		select {
		case channel <- notification:
		default:
			select {
			case <-channel:
			default:
			}
			channel <- notification
		}
	}
}
