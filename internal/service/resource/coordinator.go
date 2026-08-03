// Package resource 实现 Guest 附件的有界并发、流式落盘和安全下载。
package resource

import (
	"context"
	"sort"
	"sync"

	"meet-sieve/internal/infra/apperr"
)

const maxConcurrentUploads = 3

// UploadCoordinator 是活动上传 context 和并发容量的唯一 owner。
type UploadCoordinator struct {
	mu        sync.Mutex
	active    map[string]*uploadEntry
	bySession map[string]string
}

type uploadEntry struct {
	meetingID string
	sessionID string
	requestID string
	name      string
	written   int64
	total     int64
	cancel    context.CancelFunc
}

// UploadProgress 是宿主结束会议确认所需的安全活动上传投影。
type UploadProgress struct {
	RequestID string `json:"request_id"`
	Name      string `json:"name"`
	Written   int64  `json:"written"`
	Total     int64  `json:"total"`
}

// UploadReservation 代表一个必须释放的有界上传占用。
type UploadReservation struct {
	coordinator *UploadCoordinator
	key         string
	ctx         context.Context
	once        sync.Once
}

// NewUploadCoordinator 创建同 session 一个、全局三个的上传协调器。
func NewUploadCoordinator() *UploadCoordinator {
	return &UploadCoordinator{active: make(map[string]*uploadEntry), bySession: make(map[string]string)}
}

// Reserve 按 session/request ID 占用上传容量并派生可取消 context。
func (coordinator *UploadCoordinator) Reserve(parent context.Context, meetingID string, sessionID string, requestID string) (*UploadReservation, error) {
	return coordinator.reserve(parent, meetingID, sessionID, requestID, "", 0)
}

// ReserveAttachment 为宿主进度投影额外登记安全显示名和声明总字节。
func (coordinator *UploadCoordinator) ReserveAttachment(parent context.Context, meetingID string, sessionID string, requestID string, name string, total int64) (*UploadReservation, error) {
	return coordinator.reserve(parent, meetingID, sessionID, requestID, name, total)
}

// reserve 完成统一的并发占用和可取消 context 创建。
func (coordinator *UploadCoordinator) reserve(parent context.Context, meetingID string, sessionID string, requestID string, name string, total int64) (*UploadReservation, error) {
	if coordinator == nil || meetingID == "" || sessionID == "" || requestID == "" {
		return nil, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("resource.upload.reserve"))
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	key := sessionID + "\x00" + requestID
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if _, exists := coordinator.active[key]; exists || coordinator.bySession[sessionID] != "" || len(coordinator.active) >= maxConcurrentUploads {
		return nil, apperr.Biz(apperr.CodeLANRateLimited, apperr.WithOp("resource.upload.capacity"))
	}
	ctx, cancel := context.WithCancel(parent)
	coordinator.active[key] = &uploadEntry{meetingID: meetingID, sessionID: sessionID, requestID: requestID, name: name, total: total, cancel: cancel}
	coordinator.bySession[sessionID] = key
	return &UploadReservation{coordinator: coordinator, key: key, ctx: ctx}, nil
}

// ReportProgress 更新当前占用的真实落盘字节；乱序和超出声明值会被夹紧。
func (reservation *UploadReservation) ReportProgress(written int64) {
	if reservation == nil || reservation.coordinator == nil {
		return
	}
	reservation.coordinator.mu.Lock()
	entry := reservation.coordinator.active[reservation.key]
	if entry != nil && written >= entry.written {
		if entry.total > 0 && written > entry.total {
			written = entry.total
		}
		entry.written = written
	}
	reservation.coordinator.mu.Unlock()
}

// Snapshot 返回指定会议当前活动上传的不可变安全副本。
func (coordinator *UploadCoordinator) Snapshot(meetingID string) []UploadProgress {
	if coordinator == nil || meetingID == "" {
		return []UploadProgress{}
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	result := make([]UploadProgress, 0, len(coordinator.active))
	for _, entry := range coordinator.active {
		if entry.meetingID == meetingID {
			result = append(result, UploadProgress{RequestID: entry.requestID, Name: entry.name, Written: entry.written, Total: entry.total})
		}
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].RequestID < result[right].RequestID })
	return result
}

// Context 返回会议结束、宿主取消和客户断开共用的 context。
func (reservation *UploadReservation) Context() context.Context {
	if reservation == nil || reservation.ctx == nil {
		return context.Background()
	}
	return reservation.ctx
}

// Release 幂等释放上传占用和关联 context。
func (reservation *UploadReservation) Release() {
	if reservation == nil || reservation.coordinator == nil {
		return
	}
	reservation.once.Do(func() {
		reservation.coordinator.release(reservation.key)
	})
}

// CancelMeeting 取消指定会议的全部活动上传。
func (coordinator *UploadCoordinator) CancelMeeting(meetingID string) {
	if coordinator == nil || meetingID == "" {
		return
	}
	coordinator.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(coordinator.active))
	for _, entry := range coordinator.active {
		if entry.meetingID == meetingID {
			cancels = append(cancels, entry.cancel)
		}
	}
	coordinator.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// CancelRequest 按宿主可见 request ID 取消当前匹配上传。
func (coordinator *UploadCoordinator) CancelRequest(requestID string) bool {
	if coordinator == nil || requestID == "" {
		return false
	}
	coordinator.mu.Lock()
	var cancel context.CancelFunc
	for _, entry := range coordinator.active {
		if entry.requestID == requestID {
			cancel = entry.cancel
			break
		}
	}
	coordinator.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// release 删除已完成的有界上传记录。
func (coordinator *UploadCoordinator) release(key string) {
	coordinator.mu.Lock()
	entry := coordinator.active[key]
	if entry != nil {
		delete(coordinator.active, key)
		delete(coordinator.bySession, entry.sessionID)
	}
	coordinator.mu.Unlock()
	if entry != nil {
		entry.cancel()
	}
}
