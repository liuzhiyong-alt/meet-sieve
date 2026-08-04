// Package resourceopen 校验资源完整性，并只在用户明确命令后调用系统默认程序。
package resourceopen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// Launcher 接收已经验证的绝对文件路径或 HTTP(S) URL。
type Launcher interface {
	Open(context.Context, string) error
	Reveal(context.Context, string) error
	OpenURL(context.Context, string) error
}

// Result 是不包含路径和原始 URL 的资源打开投影。
type Result struct {
	ResourceID     string
	IntegrityState string
	VerifiedAt     int64
	Hostname       string
	Opened         bool
}

// Service 是附件 Open/Reveal 和外链打开的唯一业务入口。
type Service struct {
	reader       *gorm.DB
	transactions *database.TransactionManager
	workspace    string
	launcher     Launcher
	now          func() time.Time
}

// NewService 创建资源打开服务；构造阶段不校验或打开任何对象。
func NewService(reader *gorm.DB, transactions *database.TransactionManager, workspace string, launcher Launcher) *Service {
	return &Service{reader: reader, transactions: transactions, workspace: workspace, launcher: launcher, now: time.Now}
}

// OpenAttachment 重新校验附件并使用默认应用打开。
func (service *Service) OpenAttachment(ctx context.Context, resourceID string) (Result, error) {
	return service.openFile(ctx, resourceID, false)
}

// RevealAttachment 重新校验附件并在文件管理器中定位。
func (service *Service) RevealAttachment(ctx context.Context, resourceID string) (Result, error) {
	return service.openFile(ctx, resourceID, true)
}

// OpenExternalLink 重读原 URL，只允许 http/https 后交给默认浏览器。
func (service *Service) OpenExternalLink(ctx context.Context, resourceID string) (Result, error) {
	resource, _, err := service.readResource(ctx, resourceID)
	if err != nil || resource.Kind != "link" || resource.State != "completed" || resource.SourceURL == nil {
		return Result{}, apperr.Biz(apperr.CodeResourceMissing, apperr.WithOp("resourceopen.link.read"))
	}
	parsed, err := url.Parse(*resource.SourceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return Result{}, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("resourceopen.link.url"))
	}
	if service.launcher == nil {
		return Result{}, apperr.Sys(nil, apperr.WithOp("resourceopen.link.launcher"))
	}
	if err := service.launcher.OpenURL(ctx, parsed.String()); err != nil {
		return Result{}, apperr.Sys(err, apperr.WithOp("resourceopen.link.open"))
	}
	return Result{ResourceID: resource.ID, Hostname: parsed.Hostname(), Opened: true}, nil
}

// openFile 执行“重读事实 → 路径/类型/大小/SHA 校验 → 持久化 → 系统调用”。
func (service *Service) openFile(ctx context.Context, resourceID string, reveal bool) (Result, error) {
	resource, meeting, err := service.readResource(ctx, resourceID)
	if err != nil || resource.Kind != "attachment" || resource.State != "completed" || resource.RelativePath == nil {
		return Result{}, apperr.Biz(apperr.CodeResourceMissing, apperr.WithOp("resourceopen.file.read"))
	}
	now := service.now().UnixMilli()
	target, code, err := service.verify(ctx, resource, meeting)
	if err != nil {
		_ = service.persistIntegrity(context.Background(), resource.ID, integrityState(code), now, code.ErrorCode)
		return Result{}, apperr.Biz(code, apperr.WithOp("resourceopen.file.verify"))
	}
	if err := service.persistIntegrity(ctx, resource.ID, "verified", now, ""); err != nil {
		return Result{}, err
	}
	if service.launcher == nil {
		return Result{}, apperr.Sys(nil, apperr.WithOp("resourceopen.file.launcher"))
	}
	if reveal {
		err = service.launcher.Reveal(ctx, target)
	} else {
		err = service.launcher.Open(ctx, target)
	}
	if err != nil {
		_ = service.persistIntegrity(context.Background(), resource.ID, "unavailable", now, apperr.CodeResourceOpenFailed.ErrorCode)
		return Result{}, apperr.Sys(err, apperr.WithOp("resourceopen.file.open"))
	}
	return Result{ResourceID: resource.ID, IntegrityState: "verified", VerifiedAt: now, Opened: true}, nil
}

// readResource 返回资源和所属会议目录关系，不接受跨会议输入参数。
func (service *Service) readResource(ctx context.Context, resourceID string) (models.Resource, models.Meeting, error) {
	if service == nil || service.reader == nil || resourceID == "" {
		return models.Resource{}, models.Meeting{}, gorm.ErrRecordNotFound
	}
	var row struct {
		models.Resource
		MeetingRelativeDir string `gorm:"column:meeting_relative_dir"`
	}
	err := service.reader.WithContext(ctx).Table("resources AS resource").
		Select("resource.*, meeting.relative_dir AS meeting_relative_dir").
		Joins("JOIN meetings AS meeting ON meeting.id = resource.meeting_id").
		Where("resource.id = ?", resourceID).Take(&row).Error
	if err != nil {
		return models.Resource{}, models.Meeting{}, err
	}
	return row.Resource, models.Meeting{ID: row.Resource.MeetingID, RelativeDir: row.MeetingRelativeDir}, nil
}

// verify 拒绝路径逃逸、任意符号链接、非普通文件、大小或 SHA 变化。
func (service *Service) verify(ctx context.Context, resource models.Resource, meeting models.Meeting) (string, apperr.Code, error) {
	if !filepath.IsAbs(service.workspace) || filepath.IsAbs(meeting.RelativeDir) || filepath.IsAbs(*resource.RelativePath) {
		return "", apperr.CodeResourceOutsideWorkspace, fmt.Errorf("资源路径无效")
	}
	meetingRoot := filepath.Clean(filepath.Join(service.workspace, filepath.FromSlash(meeting.RelativeDir)))
	target := filepath.Clean(filepath.Join(meetingRoot, filepath.FromSlash(*resource.RelativePath)))
	if !withinRoot(service.workspace, meetingRoot) || !withinRoot(meetingRoot, target) || hasSymlinkComponent(service.workspace, target) {
		return "", apperr.CodeResourceOutsideWorkspace, fmt.Errorf("资源路径越界或包含符号链接")
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return "", apperr.CodeResourceMissing, err
	}
	if err != nil || !info.Mode().IsRegular() {
		return "", apperr.CodeResourceOutsideWorkspace, err
	}
	if resource.SizeBytes == nil || info.Size() != *resource.SizeBytes || resource.SHA256 == nil {
		return "", apperr.CodeResourceChanged, fmt.Errorf("资源大小变化")
	}
	digest, err := hashFile(ctx, target)
	if err != nil {
		return "", apperr.CodeResourceChanged, err
	}
	if !strings.EqualFold(digest, *resource.SHA256) {
		return "", apperr.CodeResourceChanged, fmt.Errorf("资源哈希变化")
	}
	return target, apperr.CodeOK, nil
}

// persistIntegrity 在短事务中保存最新完整性事实。
func (service *Service) persistIntegrity(ctx context.Context, resourceID string, state string, verifiedAt int64, errorCode string) error {
	if service.transactions == nil {
		return fmt.Errorf("资源完整性事务不可用")
	}
	return service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		updates := map[string]any{"integrity_state": state, "last_verified_at": verifiedAt, "integrity_error_code": nil, "updated_at": verifiedAt}
		if errorCode != "" {
			updates["integrity_error_code"] = errorCode
		}
		return tx.WithContext(ctx).Model(&models.Resource{}).Where("id = ?", resourceID).Updates(updates).Error
	})
}

// withinRoot 使用 lexical 相对路径验证 containment。
func withinRoot(root string, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// hasSymlinkComponent 使用 Lstat 检查会议根到目标的每一级组件。
func hasSymlinkComponent(root string, target string) bool {
	rootInfo, rootErr := os.Lstat(root)
	if rootErr != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return true
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return true
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// hashFile 流式计算 SHA-256，并在每个读取周期响应取消。
func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// integrityState 把稳定错误码映射为数据库 CHECK 允许的状态。
func integrityState(code apperr.Code) string {
	switch code.ErrorCode {
	case apperr.CodeResourceMissing.ErrorCode:
		return "missing"
	case apperr.CodeResourceChanged.ErrorCode:
		return "changed"
	case apperr.CodeResourceOutsideWorkspace.ErrorCode:
		return "outside_workspace"
	default:
		return "unavailable"
	}
}
