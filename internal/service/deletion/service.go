// Package deletion 编排录音和整场会议的可恢复安全删除。
package deletion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	deletiondomain "meet-sieve/internal/domain/deletion"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	deletionrepository "meet-sieve/internal/repository/deletion"
	lifecycleservice "meet-sieve/internal/service/lifecycle"
	"meet-sieve/models"
)

const maintenanceTimeout = 5 * time.Second

// Preview 是危险确认弹窗允许展示的安全删除摘要。
type Preview struct {
	MeetingID      string
	MeetingNo      string
	Kind           deletiondomain.Kind
	Revision       int64
	Digest         string
	FileCount      int
	DirectoryCount int
	SymlinkCount   int
	UnknownCount   int
	SizeBytes      int64
}

// Result 是删除执行后的持久任务投影。
type Result struct {
	JobID         string
	MeetingID     string
	Kind          deletiondomain.Kind
	State         string
	Remaining     []deletionrepository.FailedItem
	AttemptCount  int
	LastErrorCode string
}

// GetJobByMeeting 返回同场当前活动/失败删除任务；没有任务时返回 nil。
func (service *Service) GetJobByMeeting(ctx context.Context, meetingID string) (*Result, error) {
	job, err := service.repository.GetActiveByMeeting(ctx, meetingID)
	if err != nil || job == nil {
		return nil, err
	}
	result, err := service.mapJob(*job)
	return &result, err
}

// GetJob 返回指定任务的当前安全投影。
func (service *Service) GetJob(ctx context.Context, jobID string) (Result, error) {
	job, err := service.repository.Get(ctx, jobID)
	if err != nil {
		return Result{}, err
	}
	return service.mapJob(job)
}

// Dependencies 描述删除服务的明确基础设施边界。
type Dependencies struct {
	Repository    *deletionrepository.Repository
	Maintenance   *lifecycleservice.Coordinator
	IDs           identity.Generator
	Clock         clock.Clock
	WorkspaceRoot string
}

// Service 执行预览、二次校验、逐项持久化和显式重试。
type Service struct {
	repository  *deletionrepository.Repository
	maintenance *lifecycleservice.Coordinator
	ids         identity.Generator
	clock       clock.Clock
	workspace   string
	exitMu      sync.Mutex
	exiting     bool
	active      int
	idle        chan struct{}
}

// NewService 创建删除服务，不扫描或删除文件。
func NewService(dependencies Dependencies) *Service {
	idle := make(chan struct{})
	close(idle)
	return &Service{repository: dependencies.Repository, maintenance: dependencies.Maintenance, ids: dependencies.IDs, clock: dependencies.Clock, workspace: dependencies.WorkspaceRoot, idle: idle}
}

// PreviewRecording 读取真实音频资产和会议目录，返回不含路径的录音删除摘要。
func (service *Service) PreviewRecording(ctx context.Context, meetingID string) (Preview, error) {
	manifest, err := service.buildManifest(ctx, meetingID, deletiondomain.KindRecording)
	if err != nil {
		return Preview{}, err
	}
	return summarize(manifest), nil
}

// PreviewMeeting 扫描整个规范会议目录，未知文件纳入清单但不跟随符号链接。
func (service *Service) PreviewMeeting(ctx context.Context, meetingID string) (Preview, error) {
	manifest, err := service.buildManifest(ctx, meetingID, deletiondomain.KindMeeting)
	if err != nil {
		return Preview{}, err
	}
	return summarize(manifest), nil
}

// DeleteRecording 使用预览 revision/digest 创建并执行录音删除任务。
func (service *Service) DeleteRecording(ctx context.Context, meetingID string, revision int64, digest string) (Result, error) {
	return service.start(ctx, meetingID, deletiondomain.KindRecording, revision, digest, "")
}

// DeleteMeeting 使用会议号和预览事实二次确认整场永久删除。
func (service *Service) DeleteMeeting(ctx context.Context, meetingID string, meetingNo string, revision int64, digest string) (Result, error) {
	return service.start(ctx, meetingID, deletiondomain.KindMeeting, revision, digest, meetingNo)
}

// Retry 仅执行原 manifest 中失败/未完成的项目，不重新扫描扩大范围。
func (service *Service) Retry(ctx context.Context, jobID string) (Result, error) {
	if !service.enterOperation() {
		return Result{}, apperr.Biz(apperr.CodeMeetingMaintenanceLocked, apperr.WithOp("deletion.retry.exiting"))
	}
	defer service.leaveOperation()
	job, err := service.repository.Get(ctx, jobID)
	if err != nil {
		return Result{}, err
	}
	manifest, err := deletiondomain.Decode([]byte(job.TargetManifestJSON))
	if err != nil {
		return Result{}, apperr.Sys(err, apperr.WithOp("deletion.retry.manifest"))
	}
	lease, err := service.maintenance.Acquire(ctx, manifest.MeetingID, maintenanceTimeout)
	if err != nil {
		return Result{}, err
	}
	defer lease.Release()
	if err := service.repository.MarkRunning(ctx, job.ID, true, service.now()); err != nil {
		return Result{}, err
	}
	remaining := failedIDs(job, manifest.Items)
	return service.execute(ctx, job.ID, manifest, remaining, job.AttemptCount+1)
}

// RecoverInterrupted 把进程遗留 running 任务转为 failed，禁止自动续删。
func (service *Service) RecoverInterrupted(ctx context.Context) error {
	if service == nil || service.repository == nil {
		return fmt.Errorf("删除服务不可用")
	}
	return service.repository.RecoverRunning(ctx, service.now())
}

// mapJob 解析持久失败清单并返回不含内部 manifest 的任务投影。
func (service *Service) mapJob(job models.DeletionJob) (Result, error) {
	result := Result{JobID: job.ID, MeetingID: job.MeetingID, Kind: deletiondomain.Kind(job.Kind), State: job.State, AttemptCount: job.AttemptCount}
	if job.LastErrorCode != nil {
		result.LastErrorCode = *job.LastErrorCode
	}
	if job.FailedItemsJSON != nil {
		if err := json.Unmarshal([]byte(*job.FailedItemsJSON), &result.Remaining); err != nil {
			return Result{}, apperr.Sys(err, apperr.WithOp("deletion.job.failed_items"))
		}
	}
	return result, nil
}

// start 在维护锁内二次扫描，只有预览未变化才持久化并执行清单。
func (service *Service) start(ctx context.Context, meetingID string, kind deletiondomain.Kind, revision int64, digest string, meetingNo string) (Result, error) {
	if !service.enterOperation() {
		return Result{}, apperr.Biz(apperr.CodeMeetingMaintenanceLocked, apperr.WithOp("deletion.start.exiting"))
	}
	defer service.leaveOperation()
	if err := service.validate(); err != nil {
		return Result{}, err
	}
	lease, err := service.maintenance.Acquire(ctx, meetingID, maintenanceTimeout)
	if err != nil {
		return Result{}, err
	}
	defer lease.Release()
	manifest, err := service.buildManifest(ctx, meetingID, kind)
	if err != nil {
		return Result{}, err
	}
	if manifest.Revision != revision || manifest.Digest != digest || kind == deletiondomain.KindMeeting && manifest.MeetingNo != meetingNo {
		return Result{}, apperr.Biz(apperr.CodeDeletePreviewStale, apperr.WithOp("deletion.start.stale"))
	}
	data, err := deletiondomain.Encode(manifest)
	if err != nil {
		return Result{}, apperr.Sys(err, apperr.WithOp("deletion.start.encode"))
	}
	now := service.now()
	job := models.DeletionJob{ID: service.ids.New(), MeetingID: meetingID, Kind: string(kind), State: "pending", TargetManifestJSON: string(data), CreatedAt: now, UpdatedAt: now}
	if err := service.repository.Create(ctx, job); err != nil {
		if errors.Is(err, deletionrepository.ErrActiveJob) {
			return Result{}, apperr.Biz(apperr.CodeMeetingMaintenanceLocked, apperr.WithOp("deletion.start.active"))
		}
		return Result{}, err
	}
	if err := service.repository.MarkRunning(ctx, job.ID, false, service.now()); err != nil {
		return Result{}, err
	}
	return service.execute(ctx, job.ID, manifest, manifest.Items, 0)
}

// execute 逐项删除并在每个原子文件操作后持久化剩余清单。
func (service *Service) execute(ctx context.Context, jobID string, manifest deletiondomain.Manifest, candidates []deletiondomain.Item, attempt int) (Result, error) {
	meetingRoot, err := service.meetingRoot(ctx, manifest.MeetingID)
	if err != nil {
		return Result{}, err
	}
	remaining := make([]deletionrepository.FailedItem, 0)
	ordered := deletiondomain.SortForDeletion(candidates)
	for index, item := range ordered {
		if err := ctx.Err(); err != nil || service.isExiting() {
			remaining = append(remaining, toPendingItems(ordered[index:])...)
			_ = service.repository.SaveRemaining(context.Background(), jobID, remaining, service.now())
			break
		}
		if err := removeManifestItem(meetingRoot, item); err != nil {
			remaining = append(remaining, deletionrepository.FailedItem{ItemID: item.ID, SafeName: filepath.Base(item.RelativePath), Code: "DELETE_ITEM_BUSY"})
		}
		// 每个原子项后同时保存既有失败和尚未领取的项目，崩溃后不会漏删或扩大范围。
		checkpoint := append(append([]deletionrepository.FailedItem(nil), remaining...), toPendingItems(ordered[index+1:])...)
		if err := service.repository.SaveRemaining(ctx, jobID, checkpoint, service.now()); err != nil {
			return Result{}, err
		}
	}
	if len(remaining) == 0 && manifest.Kind == deletiondomain.KindMeeting {
		if err := os.Remove(meetingRoot); err != nil && !os.IsNotExist(err) {
			remaining = append(remaining, deletionrepository.FailedItem{ItemID: "meeting-root", SafeName: "会议目录", Code: "DELETE_ITEM_BUSY"})
		}
	}
	if len(remaining) > 0 {
		_ = service.repository.MarkFailed(ctx, jobID, remaining, "DELETE_ITEM_BUSY", service.now())
		return Result{JobID: jobID, MeetingID: manifest.MeetingID, Kind: manifest.Kind, State: "failed", Remaining: remaining, AttemptCount: attempt, LastErrorCode: "DELETE_ITEM_BUSY"}, nil
	}
	if manifest.Kind == deletiondomain.KindMeeting {
		err = service.repository.CompleteMeeting(ctx, jobID, manifest.MeetingID)
	} else {
		err = service.repository.CompleteRecording(ctx, jobID, manifest.MeetingID, service.now())
	}
	if err != nil {
		return Result{}, err
	}
	return Result{JobID: jobID, MeetingID: manifest.MeetingID, Kind: manifest.Kind, State: "completed", AttemptCount: attempt}, nil
}

// PrepareExit 阻止新删除和新文件项，并等待当前原子项完成持久化。
// 返回 false 表示在时限内无法确认安全状态，调用方必须阻止退出。
func (service *Service) PrepareExit(ctx context.Context) bool {
	if service == nil {
		return true
	}
	service.exitMu.Lock()
	service.exiting = true
	idle := service.idle
	service.exitMu.Unlock()
	select {
	case <-idle:
		return true
	case <-ctx.Done():
		service.exitMu.Lock()
		service.exiting = false
		service.exitMu.Unlock()
		return false
	}
}

// enterOperation 登记一个删除命令；退出门开启后拒绝新命令。
func (service *Service) enterOperation() bool {
	if service == nil {
		return false
	}
	service.exitMu.Lock()
	defer service.exitMu.Unlock()
	if service.exiting {
		return false
	}
	if service.active == 0 {
		service.idle = make(chan struct{})
	}
	service.active++
	return true
}

// leaveOperation 完成删除命令并在最后一个命令退出时关闭等待通道。
func (service *Service) leaveOperation() {
	service.exitMu.Lock()
	service.active--
	if service.active == 0 {
		close(service.idle)
	}
	service.exitMu.Unlock()
}

// isExiting 返回退出门是否已要求停止领取新文件项。
func (service *Service) isExiting() bool {
	service.exitMu.Lock()
	defer service.exitMu.Unlock()
	return service.exiting
}

// buildManifest 从 SQLite 会议关系和当前 Lstat 文件树构造不可扩大清单。
func (service *Service) buildManifest(ctx context.Context, meetingID string, kind deletiondomain.Kind) (deletiondomain.Manifest, error) {
	if err := service.validate(); err != nil {
		return deletiondomain.Manifest{}, err
	}
	meeting, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return deletiondomain.Manifest{}, apperr.Biz(apperr.CodeMeetingNotFound, apperr.WithOp("deletion.preview.meeting"))
	}
	root, err := trustedMeetingRoot(service.workspace, meeting.RelativeDir)
	if err != nil {
		return deletiondomain.Manifest{}, apperr.Biz(apperr.CodeDeletePathOutsideMeeting, apperr.WithOp("deletion.preview.root"))
	}
	assets, err := service.repository.ListAudioAssets(ctx, meetingID)
	if err != nil {
		return deletiondomain.Manifest{}, err
	}
	known := knownPaths(root, service.workspace, assets)
	items, err := deletiondomain.Scan(root, known)
	if err != nil {
		if strings.Contains(err.Error(), "特殊文件") {
			return deletiondomain.Manifest{}, apperr.Biz(apperr.CodeDeleteSpecialFileBlocked, apperr.WithOp("deletion.preview.scan"))
		}
		return deletiondomain.Manifest{}, err
	}
	if kind == deletiondomain.KindRecording {
		items = recordingItems(items)
	}
	manifest := deletiondomain.Manifest{Version: deletiondomain.ManifestVersion, MeetingID: meeting.ID, MeetingNo: meeting.MeetingNo, Kind: kind, Revision: meeting.UpdatedAt, Items: items}
	encoded, err := deletiondomain.Encode(manifest)
	if err != nil {
		return deletiondomain.Manifest{}, err
	}
	return deletiondomain.Decode(encoded)
}

// meetingRoot 重读 SQLite 关系并解析规范会议根目录。
func (service *Service) meetingRoot(ctx context.Context, meetingID string) (string, error) {
	meeting, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return "", err
	}
	return trustedMeetingRoot(service.workspace, meeting.RelativeDir)
}

// validate 校验删除服务必须依赖和工作目录。
func (service *Service) validate() error {
	if service == nil || service.repository == nil || service.maintenance == nil || service.ids == nil || service.clock == nil || !filepath.IsAbs(service.workspace) {
		return fmt.Errorf("删除服务依赖无效")
	}
	return nil
}

// now 返回统一毫秒时间。
func (service *Service) now() int64 { return service.clock.Now().UnixMilli() }

// summarize 将清单投影为不暴露文件名和路径的确认摘要。
func summarize(manifest deletiondomain.Manifest) Preview {
	result := Preview{MeetingID: manifest.MeetingID, MeetingNo: manifest.MeetingNo, Kind: manifest.Kind, Revision: manifest.Revision, Digest: manifest.Digest}
	for _, item := range manifest.Items {
		switch item.Type {
		case deletiondomain.ItemFile:
			result.FileCount++
			result.SizeBytes += item.SizeBytes
		case deletiondomain.ItemDirectory:
			result.DirectoryCount++
		case deletiondomain.ItemSymlink:
			result.SymlinkCount++
		}
		if !item.Known {
			result.UnknownCount++
		}
	}
	return result
}

// trustedMeetingRoot 拒绝绝对会议路径和工作目录逃逸。
func trustedMeetingRoot(workspace string, relativeDir string) (string, error) {
	if filepath.IsAbs(relativeDir) || relativeDir == "" {
		return "", fmt.Errorf("会议相对目录无效")
	}
	root := filepath.Clean(filepath.Join(workspace, filepath.FromSlash(relativeDir)))
	relative, err := filepath.Rel(workspace, root)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("会议目录越出工作目录")
	}
	return root, nil
}

// knownPaths 把工作目录相对资产转换为会议目录内相对路径。
func knownPaths(meetingRoot string, workspace string, assets []models.AudioAsset) map[string]bool {
	result := make(map[string]bool, len(assets))
	for _, asset := range assets {
		absolute := filepath.Join(workspace, filepath.FromSlash(asset.RelativePath))
		relative, err := filepath.Rel(meetingRoot, absolute)
		if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			result[relative] = true
		}
	}
	return result
}

// recordingItems 只保留会议 audio 子树，附件和其他会议事实文件不进入录音清单。
func recordingItems(items []deletiondomain.Item) []deletiondomain.Item {
	result := make([]deletiondomain.Item, 0, len(items))
	for _, item := range items {
		first := strings.Split(filepath.ToSlash(item.RelativePath), "/")[0]
		if first == "audio" {
			result = append(result, item)
		}
	}
	return result
}

// removeManifestItem 在每项执行前重算 lexical containment 并核对 Lstat 类型。
func removeManifestItem(root string, item deletiondomain.Item) error {
	target := filepath.Clean(filepath.Join(root, item.RelativePath))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative != item.RelativePath || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("删除目标越界")
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !matchesType(info, item.Type) {
		return fmt.Errorf("删除目标类型已变化")
	}
	if item.Type == deletiondomain.ItemFile {
		if info.Size() != item.SizeBytes {
			return fmt.Errorf("删除目标大小已变化")
		}
		digest, hashErr := deletiondomain.HashFile(target)
		if hashErr != nil || digest != item.SHA256 {
			return fmt.Errorf("删除目标内容已变化")
		}
	}
	return os.Remove(target)
}

// matchesType 核对执行时文件类型，避免预览后替换为不同对象。
func matchesType(info os.FileInfo, itemType deletiondomain.ItemType) bool {
	if info == nil {
		return false
	}
	switch itemType {
	case deletiondomain.ItemFile:
		return info.Mode().IsRegular()
	case deletiondomain.ItemDirectory:
		return info.IsDir() && info.Mode()&os.ModeSymlink == 0
	case deletiondomain.ItemSymlink:
		return info.Mode()&os.ModeSymlink != 0
	default:
		return false
	}
}

// toPendingItems 把未开始项目转换为不泄漏路径的安全剩余项。
func toPendingItems(items []deletiondomain.Item) []deletionrepository.FailedItem {
	result := make([]deletionrepository.FailedItem, 0, len(items))
	for _, item := range items {
		result = append(result, deletionrepository.FailedItem{ItemID: item.ID, SafeName: filepath.Base(item.RelativePath), Code: "DELETE_INTERRUPTED"})
	}
	return result
}

// failedIDs 从持久失败 JSON 恢复原清单子集；无清单时保守重试全部项目。
func failedIDs(job models.DeletionJob, items []deletiondomain.Item) []deletiondomain.Item {
	if job.FailedItemsJSON == nil {
		return items
	}
	var failed []deletionrepository.FailedItem
	if err := json.Unmarshal([]byte(*job.FailedItemsJSON), &failed); err != nil {
		return items
	}
	wanted := make(map[string]bool, len(failed))
	for _, item := range failed {
		wanted[item.ItemID] = true
	}
	result := make([]deletiondomain.Item, 0, len(wanted))
	for _, item := range items {
		if wanted[item.ID] {
			result = append(result, item)
		}
	}
	return result
}
