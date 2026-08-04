package wails

import (
	"context"

	resourceopenservice "meet-sieve/internal/service/resourceopen"
)

// ResourceOpenServiceProvider 返回当前工作目录资源打开服务。
type ResourceOpenServiceProvider func() (*resourceopenservice.Service, error)

// ResourceBinding 只暴露按资源 ID 的受控系统打开操作。
type ResourceBinding struct {
	service  ResourceOpenServiceProvider
	boundary *Boundary
}

// NewResourceBinding 创建资源 Binding。
func NewResourceBinding(service ResourceOpenServiceProvider, boundary *Boundary) *ResourceBinding {
	return &ResourceBinding{service: service, boundary: boundary}
}

// OpenResource 校验附件后调用系统默认应用。
func (binding *ResourceBinding) OpenResource(resourceID string) Result[ResourceOpenDTO] {
	return binding.invoke("wails.resource.open", resourceID, func(service *resourceopenservice.Service) (resourceopenservice.Result, error) {
		return service.OpenAttachment(context.Background(), resourceID)
	})
}

// RevealResource 校验附件后在文件管理器中定位。
func (binding *ResourceBinding) RevealResource(resourceID string) Result[ResourceOpenDTO] {
	return binding.invoke("wails.resource.reveal", resourceID, func(service *resourceopenservice.Service) (resourceopenservice.Result, error) {
		return service.RevealAttachment(context.Background(), resourceID)
	})
}

// OpenExternalLink 重读并验证 HTTP(S) URL 后调用默认浏览器。
func (binding *ResourceBinding) OpenExternalLink(resourceID string) Result[ResourceOpenDTO] {
	return binding.invoke("wails.resource.open_link", resourceID, func(service *resourceopenservice.Service) (resourceopenservice.Result, error) {
		return service.OpenExternalLink(context.Background(), resourceID)
	})
}

// invoke 统一执行 Resource ID 边界校验和 DTO 映射。
func (binding *ResourceBinding) invoke(operation string, resourceID string, command func(*resourceopenservice.Service) (resourceopenservice.Result, error)) Result[ResourceOpenDTO] {
	return Invoke(binding.boundary, operation, func(string) (ResourceOpenDTO, error) {
		if err := requireUUID("resource ID", resourceID); err != nil {
			return ResourceOpenDTO{}, err
		}
		service, err := binding.service()
		if err != nil {
			return ResourceOpenDTO{}, err
		}
		result, err := command(service)
		return mapResourceOpenDTO(result), err
	})
}
