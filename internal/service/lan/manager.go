package lan

import (
	"context"
	"errors"
	"fmt"

	guestdomain "meet-sieve/internal/domain/guest"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	meetingrepository "meet-sieve/internal/repository/meeting"
)

// InterfaceResolver 是 LAN Manager 消费的平台网卡解析边界。
type InterfaceResolver interface {
	Resolve(context.Context) (guestdomain.InterfaceResolution, error)
}

// Manager 编排网卡选择、Runtime 启停和独立数据库 LAN 状态轴。
type Manager struct {
	resolver InterfaceResolver
	runtime  *Runtime
	meetings *meetingrepository.Repository
	clock    clock.Clock
}

// InterfaceList 是宿主选择页所需的安全网卡结果。
type InterfaceList struct {
	Choices       []guestdomain.NetworkInterface
	RecommendedID string
	Reason        guestdomain.SelectionReason
	Warning       string
}

// NewManager 创建不自动猜测网卡的 LAN 管理服务。
func NewManager(resolver InterfaceResolver, runtime *Runtime, meetings *meetingrepository.Repository, currentClock clock.Clock) *Manager {
	return &Manager{resolver: resolver, runtime: runtime, meetings: meetings, clock: currentClock}
}

// ListInterfaces 返回所有安全私网候选，只在默认路由明确时设置推荐。
func (manager *Manager) ListInterfaces(ctx context.Context) (InterfaceList, error) {
	if manager == nil || manager.resolver == nil {
		return InterfaceList{}, fmt.Errorf("LAN 网卡解析器不可用")
	}
	resolved, err := manager.resolver.Resolve(ctx)
	if err != nil {
		return InterfaceList{}, apperr.Dependency(apperr.CodeLANInterfaceUnavailable, err, apperr.WithOp("lan.manager.resolve"))
	}
	selection := guestdomain.SelectPrivateInterfaces(resolved.Interfaces)
	result := InterfaceList{Choices: selection.Choices, Reason: selection.Reason, Warning: resolved.Warning}
	if selection.Recommended != nil {
		result.RecommendedID = selection.Recommended.ID
	}
	return result, nil
}

// StartMeeting 按用户确认的 interface ID 启动本场 LAN generation。
func (manager *Manager) StartMeeting(ctx context.Context, meetingID string, interfaceID string) error {
	if manager == nil || manager.runtime == nil || manager.meetings == nil || manager.clock == nil || meetingID == "" || interfaceID == "" {
		return apperr.Biz(apperr.CodeLANInterfaceUnavailable, apperr.WithOp("lan.manager.start_input"))
	}
	interfaces, err := manager.ListInterfaces(ctx)
	if err != nil {
		_ = manager.meetings.UpdateLANState(ctx, meetingID, "failed", manager.clock.Now().UnixMilli())
		return err
	}
	selected, found := findInterface(interfaces.Choices, interfaceID)
	if !found {
		_ = manager.meetings.UpdateLANState(ctx, meetingID, "failed", manager.clock.Now().UnixMilli())
		return apperr.Biz(apperr.CodeLANInterfaceUnavailable, apperr.WithOp("lan.manager.interface_missing"))
	}
	now := manager.clock.Now().UnixMilli()
	_ = manager.meetings.UpdateLANState(ctx, meetingID, "starting", now)
	_, err = manager.runtime.Start(ctx, StartRequest{MeetingID: meetingID, InterfaceID: interfaceID, Address: selected.Address})
	state := "serving"
	if err != nil {
		state = "failed"
	}
	_ = manager.meetings.UpdateLANState(ctx, meetingID, state, manager.clock.Now().UnixMilli())
	return err
}

// StopMeeting 停止当前入口，并仅把对应会议 LAN 状态设为 stopped。
func (manager *Manager) StopMeeting(ctx context.Context, meetingID string) error {
	if manager == nil || manager.runtime == nil {
		return nil
	}
	err := manager.runtime.Stop(ctx)
	if manager.meetings != nil && manager.clock != nil && meetingID != "" {
		err = errors.Join(err, manager.meetings.UpdateLANState(ctx, meetingID, "stopped", manager.clock.Now().UnixMilli()))
	}
	return err
}

// Retry 为当前会议停止旧 generation 后使用新网卡重新启动。
func (manager *Manager) Retry(ctx context.Context, meetingID string, interfaceID string) error {
	if err := manager.StopMeeting(ctx, meetingID); err != nil {
		return err
	}
	return manager.StartMeeting(ctx, meetingID, interfaceID)
}

// Snapshot 返回宿主 UI 可安全展示的运行状态。
func (manager *Manager) Snapshot() Snapshot {
	if manager == nil || manager.runtime == nil {
		return Snapshot{State: StateDisabled}
	}
	return manager.runtime.Snapshot()
}

// findInterface 按不泄漏 MAC 的稳定 ID 找到用户选中候选。
func findInterface(choices []guestdomain.NetworkInterface, interfaceID string) (guestdomain.NetworkInterface, bool) {
	for _, candidate := range choices {
		if candidate.ID == interfaceID {
			return candidate, true
		}
	}
	return guestdomain.NetworkInterface{}, false
}
