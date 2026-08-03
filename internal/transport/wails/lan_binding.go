package wails

import (
	"context"
	"time"

	guestdomain "meet-sieve/internal/domain/guest"
	"meet-sieve/internal/infra/apperr"
	lanservice "meet-sieve/internal/service/lan"
	meetingservice "meet-sieve/internal/service/meeting"
	resourceservice "meet-sieve/internal/service/resource"
	guesthttp "meet-sieve/internal/transport/http/guest"
)

// LANServiceProvider 返回当前工作目录唯一的 LAN 运行时依赖。
type LANServiceProvider func() (*lanservice.Manager, *guesthttp.Presence, *resourceservice.UploadCoordinator, *meetingservice.Service, error)

// LANBinding 暴露宿主端网卡选择、状态查询、重试、停止和上传取消契约。
type LANBinding struct {
	services        LANServiceProvider
	contextProvider ContextProvider
	boundary        *Boundary
}

// LANInterfaceDTO 是不泄漏 MAC 地址的宿主网卡投影。
type LANInterfaceDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Address      string `json:"address"`
	DefaultRoute bool   `json:"default_route"`
}

// LANInterfaceListDTO 是显式网卡选择所需的完整结果。
type LANInterfaceListDTO struct {
	Interfaces    []LANInterfaceDTO `json:"interfaces"`
	RecommendedID string            `json:"recommended_id,omitempty"`
	Reason        string            `json:"reason"`
	Warning       string            `json:"warning,omitempty"`
}

// LANStatusDTO 是与录音、保存、ASR 分离的 LAN 状态轴。
type LANStatusDTO struct {
	State         string         `json:"state"`
	MeetingID     string         `json:"meeting_id,omitempty"`
	InterfaceID   string         `json:"interface_id,omitempty"`
	Address       string         `json:"address,omitempty"`
	JoinURL       string         `json:"join_url,omitempty"`
	ErrorCode     string         `json:"error_code,omitempty"`
	OnlineCount   int            `json:"online_count"`
	ActiveUploads []LANUploadDTO `json:"active_uploads"`
}

// LANUploadDTO 是宿主结束会议确认所需的活动上传进度。
type LANUploadDTO struct {
	RequestID string `json:"request_id"`
	Name      string `json:"name"`
	Written   int64  `json:"written"`
	Total     int64  `json:"total"`
}

// CancelUploadDTO 明确返回是否找到并取消对应的活动上传。
type CancelUploadDTO struct {
	Cancelled bool `json:"cancelled"`
}

// NewLANBinding 创建 LAN Binding，不在构造阶段访问数据库或枚举网卡。
func NewLANBinding(services LANServiceProvider, contextProvider ContextProvider, boundary *Boundary) *LANBinding {
	return &LANBinding{services: services, contextProvider: contextProvider, boundary: boundary}
}

// ListLANInterfaces 返回当前安全私网候选和明确的默认路由推荐。
func (binding *LANBinding) ListLANInterfaces() Result[LANInterfaceListDTO] {
	return Invoke(binding.boundary, "wails.lan.interfaces", func(_ string) (LANInterfaceListDTO, error) {
		manager, _, _, _, err := binding.services()
		if err != nil {
			return LANInterfaceListDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return LANInterfaceListDTO{}, err
		}
		result, err := manager.ListInterfaces(ctx)
		if err != nil {
			return LANInterfaceListDTO{}, err
		}
		return mapLANInterfaceList(result), nil
	})
}

// GetLANStatus 返回运行时状态与最近 45 秒活动访客数。
func (binding *LANBinding) GetLANStatus() Result[LANStatusDTO] {
	return Invoke(binding.boundary, "wails.lan.status", func(_ string) (LANStatusDTO, error) {
		manager, presence, uploads, _, err := binding.services()
		if err != nil {
			return LANStatusDTO{}, err
		}
		status := mapLANStatus(manager.Snapshot())
		if presence != nil {
			status.OnlineCount = presence.Count(time.Now())
		}
		status.ActiveUploads = mapLANUploads(uploads, status.MeetingID)
		return status, nil
	})
}

// RetryLAN 为当前活动会议停止旧 generation 后按新网卡重启。
func (binding *LANBinding) RetryLAN(interfaceID string) Result[LANStatusDTO] {
	return Invoke(binding.boundary, "wails.lan.retry", func(_ string) (LANStatusDTO, error) {
		manager, presence, _, meetings, err := binding.services()
		if err != nil {
			return LANStatusDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return LANStatusDTO{}, err
		}
		meetingID, err := activeMeetingID(ctx, meetings)
		if err != nil {
			return LANStatusDTO{}, err
		}
		if err = manager.Retry(ctx, meetingID, interfaceID); err != nil {
			return LANStatusDTO{}, err
		}
		status := mapLANStatus(manager.Snapshot())
		if presence != nil {
			presence.Reset()
		}
		return status, nil
	})
}

// StopLAN 幂等停止当前活动会议的 LAN 入口。
func (binding *LANBinding) StopLAN() Result[LANStatusDTO] {
	return Invoke(binding.boundary, "wails.lan.stop", func(_ string) (LANStatusDTO, error) {
		manager, presence, _, meetings, err := binding.services()
		if err != nil {
			return LANStatusDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return LANStatusDTO{}, err
		}
		meetingID, err := activeMeetingID(ctx, meetings)
		if err != nil {
			return LANStatusDTO{}, err
		}
		if err = manager.StopMeeting(ctx, meetingID); err != nil {
			return LANStatusDTO{}, err
		}
		if presence != nil {
			presence.Reset()
		}
		return mapLANStatus(manager.Snapshot()), nil
	})
}

// CancelGuestUpload 按请求 ID 取消仍在进行的真实流式上传。
func (binding *LANBinding) CancelGuestUpload(requestID string) Result[CancelUploadDTO] {
	return Invoke(binding.boundary, "wails.lan.upload.cancel", func(_ string) (CancelUploadDTO, error) {
		_, _, uploads, _, err := binding.services()
		if err != nil {
			return CancelUploadDTO{}, err
		}
		return CancelUploadDTO{Cancelled: uploads != nil && uploads.CancelRequest(requestID)}, nil
	})
}

// currentContext 返回桌面生命周期根 context。
func (binding *LANBinding) currentContext() (context.Context, error) {
	if binding == nil || binding.contextProvider == nil {
		return nil, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("wails.lan.context"))
	}
	ctx := binding.contextProvider()
	if ctx == nil {
		return nil, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("wails.lan.context_nil"))
	}
	return ctx, nil
}

// activeMeetingID 返回可操作 LAN 的当前会议 ID。
func activeMeetingID(ctx context.Context, meetings *meetingservice.Service) (string, error) {
	if meetings == nil {
		return "", apperr.Biz(apperr.CodeLANMeetingEnded, apperr.WithOp("wails.lan.meeting_service"))
	}
	active, err := meetings.GetActiveMeeting(ctx)
	if err != nil {
		return "", err
	}
	if active == nil {
		return "", apperr.Biz(apperr.CodeLANMeetingEnded, apperr.WithOp("wails.lan.no_active_meeting"))
	}
	return active.ID, nil
}

// mapLANInterfaceList 将领域候选映射为稳定前端契约。
func mapLANInterfaceList(source lanservice.InterfaceList) LANInterfaceListDTO {
	items := make([]LANInterfaceDTO, 0, len(source.Choices))
	for _, item := range source.Choices {
		items = append(items, mapLANInterface(item))
	}
	return LANInterfaceListDTO{Interfaces: items, RecommendedID: source.RecommendedID, Reason: string(source.Reason), Warning: source.Warning}
}

// mapLANInterface 去除领域内部信息，只保留宿主可展示字段。
func mapLANInterface(source guestdomain.NetworkInterface) LANInterfaceDTO {
	return LANInterfaceDTO{ID: source.ID, Name: source.Name, Address: source.Address, DefaultRoute: source.DefaultRoute}
}

// mapLANStatus 不向前端暴露 generation 和会议入口令牌字段之外的内部状态。
func mapLANStatus(source lanservice.Snapshot) LANStatusDTO {
	return LANStatusDTO{
		State: string(source.State), MeetingID: source.MeetingID, InterfaceID: source.InterfaceID,
		Address: source.Address, JoinURL: source.JoinURL, ErrorCode: source.ErrorCode, ActiveUploads: []LANUploadDTO{},
	}
}

// mapLANUploads 返回指定会议的活动上传安全投影。
func mapLANUploads(coordinator *resourceservice.UploadCoordinator, meetingID string) []LANUploadDTO {
	if coordinator == nil || meetingID == "" {
		return []LANUploadDTO{}
	}
	progress := coordinator.Snapshot(meetingID)
	result := make([]LANUploadDTO, 0, len(progress))
	for _, item := range progress {
		result = append(result, LANUploadDTO{RequestID: item.RequestID, Name: item.Name, Written: item.Written, Total: item.Total})
	}
	return result
}
