package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	guestdomain "meet-sieve/internal/domain/guest"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	contentrepository "meet-sieve/internal/repository/content"
	guestservice "meet-sieve/internal/service/guest"
	"meet-sieve/models"

	crerrors "github.com/cockroachdb/errors"
	"gorm.io/gorm"
)

const (
	streamDiskCheckBytes = int64(32 * 1024 * 1024)
	maxDescriptionBytes  = 2_000
)

// AvailableBytesReader 返回指定会议目录所在卷的可用字节。
type AvailableBytesReader func(string) (uint64, error)

// MeetingDirectoryResolver 只从宿主可信会议事实解析绝对目录。
type MeetingDirectoryResolver interface {
	ResolveMeetingDirectory(context.Context, string) (string, error)
}

// AttachmentDependencies 是附件流式落盘与短事务提交的显式依赖。
type AttachmentDependencies struct {
	Repository        *contentrepository.Repository
	Transactions      *database.TransactionManager
	Coordinator       *UploadCoordinator
	Policy            *FilePolicy
	Directories       MeetingDirectoryResolver
	Clock             clock.Clock
	IDs               identity.Generator
	AvailableBytes    AvailableBytesReader
	MinimumFreeBytes  uint64
	OnPersisted       func(string)
	OnTimelineChanged func(string, int64, string)
}

// AttachmentService 编排 reserve、preflight、stream、validate、rename 和 DB commit。
type AttachmentService struct {
	repository        *contentrepository.Repository
	transactions      *database.TransactionManager
	coordinator       *UploadCoordinator
	policy            *FilePolicy
	directories       MeetingDirectoryResolver
	clock             clock.Clock
	ids               identity.Generator
	availableBytes    AvailableBytesReader
	minimumFreeBytes  uint64
	onPersisted       func(string)
	onTimelineChanged func(string, int64, string)
}

// AttachmentInput 包含 HTTP 边界校验后的单文件流和声明元数据。
type AttachmentInput struct {
	RequestID         string
	OriginalName      string
	DeclaredSize      int64
	DeclaredMediaType string
	Description       string
	Reader            io.Reader
}

// AttachmentResult 是已完成附件的 Guest 安全投影。
type AttachmentResult struct {
	ResourceID   string `json:"resource_id"`
	Seq          int64  `json:"seq"`
	OccurredAt   int64  `json:"occurred_at"`
	OriginalName string `json:"original_name"`
	MediaType    string `json:"media_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

// NewAttachmentService 创建不在 SQLite 事务内执行文件 I/O 的附件服务。
func NewAttachmentService(dependencies AttachmentDependencies) *AttachmentService {
	return &AttachmentService{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		coordinator: dependencies.Coordinator, policy: dependencies.Policy, directories: dependencies.Directories,
		clock: dependencies.Clock, ids: dependencies.IDs, availableBytes: dependencies.AvailableBytes,
		minimumFreeBytes:  dependencies.MinimumFreeBytes,
		onPersisted:       dependencies.OnPersisted,
		onTimelineChanged: dependencies.OnTimelineChanged,
	}
}

// Upload 在固定内存中流式保存附件，只在最终文件存在后创建 Resource/event。
func (service *AttachmentService) Upload(ctx context.Context, authenticated guestservice.AuthenticatedSession, input AttachmentInput) (AttachmentResult, error) {
	return service.upload(ctx, attachmentOwner{
		meetingID: authenticated.Session.MeetingID,
		sessionID: authenticated.Session.ID,
		source:    "guest",
	}, input)
}

// UploadHost 使用同一安全文件链路提交主持人从系统窗口选择的附件。
func (service *AttachmentService) UploadHost(ctx context.Context, meetingID string, input AttachmentInput) (AttachmentResult, error) {
	return service.upload(ctx, attachmentOwner{
		meetingID: meetingID,
		sessionID: "host:" + meetingID,
		source:    "host",
	}, input)
}

type attachmentOwner struct {
	meetingID string
	sessionID string
	source    string
}

// upload 执行 Host 和 Guest 共用的流式文件主流程，身份校验仍在事务边界内区分。
func (service *AttachmentService) upload(ctx context.Context, owner attachmentOwner, input AttachmentInput) (AttachmentResult, error) {
	if err := service.validateInput(input); err != nil {
		return AttachmentResult{}, err
	}
	if owner.meetingID == "" || owner.sessionID == "" || (owner.source != "guest" && owner.source != "host") {
		return AttachmentResult{}, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("resource.attachment.owner"))
	}
	reservation, err := service.coordinator.ReserveAttachment(ctx, owner.meetingID, owner.sessionID, input.RequestID, input.OriginalName, input.DeclaredSize)
	if err != nil {
		return AttachmentResult{}, err
	}
	defer reservation.Release()
	meetingDirectory, err := service.directories.ResolveMeetingDirectory(reservation.Context(), owner.meetingID)
	if err != nil {
		return AttachmentResult{}, mapAttachmentError(err)
	}
	existing, err := service.findExisting(reservation.Context(), owner, input.RequestID)
	if err != nil {
		return AttachmentResult{}, mapAttachmentError(err)
	}
	if existing != nil && (existing.Kind != "attachment" || existing.OriginalName != input.OriginalName || existing.SizeBytes != input.DeclaredSize) {
		return AttachmentResult{}, apperr.Biz(apperr.CodeConflict, apperr.WithOp("resource.attachment.idempotency_metadata"))
	}
	staged, err := service.streamToStaging(reservation.Context(), meetingDirectory, input, reservation.ReportProgress)
	if err != nil {
		return AttachmentResult{}, mapAttachmentError(err)
	}
	defer staged.cleanup()
	if existing != nil {
		if existing.SHA256 != staged.stream.SHA256 {
			return AttachmentResult{}, apperr.Biz(apperr.CodeConflict, apperr.WithOp("resource.attachment.idempotency_digest"))
		}
		return resultFromExistingAttachment(*existing), nil
	}
	return service.commitStaged(reservation.Context(), owner, input, staged)
}

type stagedAttachment struct {
	partPath      string
	resourcesPath string
	validation    FileValidation
	stream        StreamResult
	committed     bool
}

// cleanup 只删除本次上传创建且尚未提交的 `.part` 文件。
func (staged *stagedAttachment) cleanup() {
	if staged != nil && !staged.committed && staged.partPath != "" {
		_ = os.Remove(staged.partPath)
	}
}

// validateInput 在创建任何文件前校验幂等键、大小、名称和说明。
func (service *AttachmentService) validateInput(input AttachmentInput) error {
	if service == nil || service.repository == nil || service.transactions == nil || service.coordinator == nil ||
		service.policy == nil || service.directories == nil || service.clock == nil || service.ids == nil ||
		service.availableBytes == nil || input.Reader == nil {
		return fmt.Errorf("附件服务依赖或输入无效")
	}
	if err := guestdomain.ValidateRequestID(input.RequestID); err != nil {
		return err
	}
	if err := ValidateDeclaredSize(input.DeclaredSize); err != nil {
		return err
	}
	if _, _, err := validateDisplayFilename(input.OriginalName); err != nil {
		return err
	}
	if !utf8.ValidString(input.Description) || len(input.Description) > maxDescriptionBytes || strings.ContainsRune(input.Description, '\x00') {
		return apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("resource.attachment.description"))
	}
	return nil
}

// streamToStaging 在会议自有 `.staging` 目录中创建 0600 part，同时复查磁盘余量。
func (service *AttachmentService) streamToStaging(ctx context.Context, meetingDirectory string, input AttachmentInput, progress func(int64)) (*stagedAttachment, error) {
	resourcesPath, stagingPath, err := prepareResourceDirectories(meetingDirectory)
	if err != nil {
		return nil, err
	}
	if err := service.ensureDiskSpace(resourcesPath, input.DeclaredSize); err != nil {
		return nil, err
	}
	partID := service.ids.New()
	if partID == "" {
		return nil, fmt.Errorf("生成附件暂存 ID 失败")
	}
	partPath := filepath.Join(stagingPath, partID+".part")
	file, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建附件暂存文件：%w", err)
	}
	staged := &stagedAttachment{partPath: partPath, resourcesPath: resourcesPath}
	stream, streamErr := service.copyWithDiskChecks(ctx, file, input.Reader, input.DeclaredSize, resourcesPath, progress)
	if streamErr == nil {
		streamErr = file.Sync()
	}
	closeErr := file.Close()
	if streamErr != nil || closeErr != nil {
		staged.cleanup()
		return nil, errors.Join(streamErr, closeErr)
	}
	validation, err := service.policy.Validate(input.OriginalName, input.DeclaredMediaType, stream.Head)
	if err != nil {
		staged.cleanup()
		return nil, err
	}
	staged.stream = stream
	staged.validation = validation
	return staged, nil
}

// copyWithDiskChecks 以固定 1 MiB buffer 复制，并每 32 MiB 检查剩余内容不侵占录音保底。
func (service *AttachmentService) copyWithDiskChecks(ctx context.Context, destination io.Writer, source io.Reader, declared int64, path string, progress func(int64)) (StreamResult, error) {
	return copyExactAndHashWithCallbacks(ctx, destination, source, declared, MaxAttachmentBytes, func(written int64) error {
		remaining := declared - written
		if remaining < 0 {
			remaining = 0
		}
		return service.ensureDiskSpace(path, remaining)
	}, progress)
}

// ensureDiskSpace 保留录音最低空间，附件不能抢占该余量。
func (service *AttachmentService) ensureDiskSpace(path string, remaining int64) error {
	available, err := service.availableBytes(path)
	if err != nil {
		return fmt.Errorf("读取附件磁盘余量：%w", err)
	}
	required := uint64(remaining) + service.minimumFreeBytes
	if available < required {
		return apperr.Biz(apperr.CodeAttachmentDiskLow, apperr.WithOp("resource.attachment.disk_space"))
	}
	return nil
}

// findExisting 在短事务中查询已完成的同 request ID 内容。
func (service *AttachmentService) findExisting(ctx context.Context, owner attachmentOwner, requestID string) (*contentrepository.ExistingContent, error) {
	var existing *contentrepository.ExistingContent
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if owner.source == "guest" {
			if _, err := service.repository.GetWritableSession(ctx, tx, owner.meetingID, owner.sessionID); err != nil {
				return err
			}
			found, err := service.repository.FindExisting(ctx, tx, owner.sessionID, requestID)
			existing = found
			return err
		}
		if _, err := service.repository.GetWritableMeeting(ctx, tx, owner.meetingID); err != nil {
			return err
		}
		found, err := service.repository.FindExistingHost(ctx, tx, owner.meetingID, requestID)
		existing = found
		return err
	})
	return existing, err
}

// commitStaged 先原子 rename 到内部 UUID 文件，再以短事务写 Resource/event；DB 失败回滚文件。
func (service *AttachmentService) commitStaged(ctx context.Context, owner attachmentOwner, input AttachmentInput, staged *stagedAttachment) (AttachmentResult, error) {
	resourceID := service.ids.New()
	eventID := service.ids.New()
	if resourceID == "" || eventID == "" {
		return AttachmentResult{}, mapAttachmentError(fmt.Errorf("生成附件实体 ID 失败"))
	}
	safeName := resourceID + staged.validation.Extension
	finalPath := filepath.Join(staged.resourcesPath, safeName)
	if err := os.Rename(staged.partPath, finalPath); err != nil {
		return AttachmentResult{}, mapAttachmentError(fmt.Errorf("原子提交附件：%w", err))
	}
	staged.committed = true
	result, err := service.commitDatabase(ctx, owner, input, staged, resourceID, eventID, safeName)
	if err != nil {
		_ = os.Remove(finalPath)
		return AttachmentResult{}, mapAttachmentError(err)
	}
	if service.onPersisted != nil {
		service.onPersisted(owner.meetingID)
	}
	if service.onTimelineChanged != nil {
		service.onTimelineChanged(owner.meetingID, result.Seq, "resource_created")
	}
	return result, nil
}

// commitDatabase 只在最终文件存在后分配 seq 并提交 Resource/event。
func (service *AttachmentService) commitDatabase(
	ctx context.Context,
	owner attachmentOwner,
	input AttachmentInput,
	staged *stagedAttachment,
	resourceID string,
	eventID string,
	safeName string,
) (AttachmentResult, error) {
	var result AttachmentResult
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		guestSessionID, err := service.validateOwnerForCommit(ctx, tx, owner)
		if err != nil {
			return err
		}
		existing, err := service.findExistingForCommit(ctx, tx, owner, input.RequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			return apperr.Biz(apperr.CodeConflict, apperr.WithOp("resource.attachment.concurrent_commit"))
		}
		seq, err := service.repository.NextEventSeq(ctx, tx, owner.meetingID)
		if err != nil {
			return err
		}
		now := service.clock.Now().UnixMilli()
		resource := buildAttachmentResource(resourceID, eventID, safeName, input, staged, owner.meetingID, guestSessionID, now)
		entityType := "resource"
		event := models.MeetingEvent{
			ID: eventID, MeetingID: owner.meetingID, Seq: seq, Kind: "resource.created", OccurredAt: now,
			Source: owner.source, EntityType: &entityType, EntityID: &resourceID, CreatedAt: now, UpdatedAt: now,
		}
		if err := service.repository.CreateLink(ctx, tx, event, resource); err != nil {
			return err
		}
		result = AttachmentResult{
			ResourceID: resourceID, Seq: seq, OccurredAt: now, OriginalName: staged.validation.DisplayName,
			MediaType: staged.validation.MediaType, SizeBytes: staged.stream.SizeBytes, SHA256: staged.stream.SHA256,
		}
		return nil
	})
	return result, err
}

// validateOwnerForCommit 在最终文件存在后再次确认会议或 Guest session 仍可写。
func (service *AttachmentService) validateOwnerForCommit(ctx context.Context, tx *gorm.DB, owner attachmentOwner) (*string, error) {
	if owner.source == "guest" {
		session, err := service.repository.GetWritableSession(ctx, tx, owner.meetingID, owner.sessionID)
		if err != nil {
			return nil, err
		}
		return &session.ID, nil
	}
	_, err := service.repository.GetWritableMeeting(ctx, tx, owner.meetingID)
	return nil, err
}

// findExistingForCommit 按 Host 或 Guest 的幂等范围读取首次提交结果。
func (service *AttachmentService) findExistingForCommit(ctx context.Context, tx *gorm.DB, owner attachmentOwner, requestID string) (*contentrepository.ExistingContent, error) {
	if owner.source == "guest" {
		return service.repository.FindExisting(ctx, tx, owner.sessionID, requestID)
	}
	return service.repository.FindExistingHost(ctx, tx, owner.meetingID, requestID)
}

// buildAttachmentResource 构建只使用会议目录相对路径的 completed Resource。
func buildAttachmentResource(
	id string,
	eventID string,
	safeName string,
	input AttachmentInput,
	staged *stagedAttachment,
	meetingID string,
	guestSessionID *string,
	now int64,
) models.Resource {
	relativePath := filepath.ToSlash(filepath.Join("resources", safeName))
	description := strings.TrimSpace(input.Description)
	var descriptionPointer *string
	if description != "" {
		descriptionPointer = &description
	}
	return models.Resource{
		ID: id, MeetingID: meetingID, EventID: eventID, GuestSessionID: guestSessionID, RequestID: &input.RequestID,
		Kind: "attachment", OriginalName: &staged.validation.DisplayName, SafeName: &safeName, RelativePath: &relativePath,
		MediaType: &staged.validation.MediaType, SizeBytes: &staged.stream.SizeBytes, SHA256: &staged.stream.SHA256,
		OriginalDescription: descriptionPointer, CurrentDescription: descriptionPointer, DescriptionRevision: 1,
		State: "completed", CreatedAt: now, UpdatedAt: now,
	}
}

// prepareResourceDirectories 只在可信绝对会议目录内创建应用自有 resources/.staging。
func prepareResourceDirectories(meetingDirectory string) (string, string, error) {
	if !filepath.IsAbs(meetingDirectory) {
		return "", "", fmt.Errorf("会议目录必须是绝对路径")
	}
	resourcesPath := filepath.Join(filepath.Clean(meetingDirectory), "resources")
	stagingPath := filepath.Join(resourcesPath, ".staging")
	if err := os.MkdirAll(stagingPath, 0o700); err != nil {
		return "", "", fmt.Errorf("创建附件暂存目录：%w", err)
	}
	for _, path := range []string{resourcesPath, stagingPath} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("附件目录不安全")
		}
	}
	return resourcesPath, stagingPath, nil
}

// resultFromExistingAttachment 在内容摘要一致时返回首次已提交结果。
func resultFromExistingAttachment(existing contentrepository.ExistingContent) AttachmentResult {
	return AttachmentResult{
		ResourceID: existing.EntityID, Seq: existing.Seq, OccurredAt: existing.OccurredAt,
		OriginalName: existing.OriginalName, MediaType: existing.MediaType, SizeBytes: existing.SizeBytes, SHA256: existing.SHA256,
	}
}

// mapAttachmentError 保留已登记业务错误，取消和未知文件/SQLite 失败分别归一。
func mapAttachmentError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *apperr.AppError
	if crerrors.As(err, &appErr) {
		return appErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return apperr.Biz(apperr.CodeAttachmentUploadCancelled, apperr.WithOp("resource.attachment.cancelled"))
	}
	if errors.Is(err, contentrepository.ErrSessionInactive) {
		return apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("resource.attachment.session"))
	}
	if errors.Is(err, contentrepository.ErrMeetingNotWritable) {
		return apperr.Biz(apperr.CodeLANMeetingEnded, apperr.WithOp("resource.attachment.meeting"))
	}
	return apperr.Sys(err, apperr.WithOp("resource.attachment.upload"))
}
