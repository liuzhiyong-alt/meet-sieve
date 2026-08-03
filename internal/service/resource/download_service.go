package resource

import (
	"context"
	"os"

	"meet-sieve/internal/infra/apperr"
	contentrepository "meet-sieve/internal/repository/content"
	guestservice "meet-sieve/internal/service/guest"
	"meet-sieve/models"
)

// DownloadService 按当前已认证会议打开 completed 附件。
type DownloadService struct {
	repository  *contentrepository.Repository
	directories MeetingDirectoryResolver
	files       *FileStore
}

// OpenedAttachment 是 HTTP 边界输出 Range 所需的文件和安全元数据。
type OpenedAttachment struct {
	File     *os.File
	Info     os.FileInfo
	Resource models.Resource
}

// NewDownloadService 创建安全附件下载服务。
func NewDownloadService(repository *contentrepository.Repository, directories MeetingDirectoryResolver, files *FileStore) *DownloadService {
	return &DownloadService{repository: repository, directories: directories, files: files}
}

// Open 先按会议/Resource ID 查库，再用相对路径安全打开普通文件。
func (service *DownloadService) Open(ctx context.Context, authenticated guestservice.AuthenticatedSession, resourceID string) (OpenedAttachment, error) {
	if service == nil || service.repository == nil || service.directories == nil || service.files == nil || resourceID == "" {
		return OpenedAttachment{}, apperr.Biz(apperr.CodeAttachmentNotFound, apperr.WithOp("resource.download.input"))
	}
	resource, err := service.repository.GetCompletedAttachment(ctx, authenticated.Session.MeetingID, resourceID)
	if err != nil {
		return OpenedAttachment{}, apperr.Sys(err, apperr.WithOp("resource.download.query"))
	}
	if resource == nil || resource.RelativePath == nil || resource.OriginalName == nil {
		return OpenedAttachment{}, apperr.Biz(apperr.CodeAttachmentNotFound, apperr.WithOp("resource.download.not_found"))
	}
	directory, err := service.directories.ResolveMeetingDirectory(ctx, authenticated.Session.MeetingID)
	if err != nil {
		return OpenedAttachment{}, apperr.Biz(apperr.CodeAttachmentNotFound, apperr.WithOp("resource.download.directory"))
	}
	file, info, err := service.files.Open(directory, *resource.RelativePath)
	if err != nil {
		return OpenedAttachment{}, apperr.Biz(apperr.CodeAttachmentNotFound, apperr.WithOp("resource.download.file"))
	}
	if resource.SizeBytes != nil && info.Size() != *resource.SizeBytes {
		_ = file.Close()
		return OpenedAttachment{}, apperr.Biz(apperr.CodeAttachmentNotFound, apperr.WithOp("resource.download.size"))
	}
	return OpenedAttachment{File: file, Info: info, Resource: *resource}, nil
}

// MediaType 返回已入库 MIME，缺失时使用二进制下载类型。
func (attachment OpenedAttachment) MediaType() string {
	if attachment.Resource.MediaType == nil || *attachment.Resource.MediaType == "" {
		return "application/octet-stream"
	}
	return *attachment.Resource.MediaType
}

// OriginalName 返回只用于 Content-Disposition 的显示名。
func (attachment OpenedAttachment) OriginalName() string {
	if attachment.Resource.OriginalName == nil {
		return "attachment"
	}
	return *attachment.Resource.OriginalName
}
