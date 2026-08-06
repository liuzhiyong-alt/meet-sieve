package wails

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	deletionservice "meet-sieve/internal/service/deletion"
)

// DeletionServiceProvider 返回当前工作目录对应的删除服务。
type DeletionServiceProvider func() (*deletionservice.Service, error)

// DeletionBinding 暴露整场会议删除与恢复命令。
type DeletionBinding struct {
	service  DeletionServiceProvider
	boundary *Boundary
}

// NewDeletionBinding 创建删除 Binding。
func NewDeletionBinding(service DeletionServiceProvider, boundary *Boundary) *DeletionBinding {
	return &DeletionBinding{service: service, boundary: boundary}
}

// PreviewMeetingDeletion 扫描整场规范目录并返回未知文件数量。
func (binding *DeletionBinding) PreviewMeetingDeletion(meetingID string) Result[DeletionPreviewDTO] {
	return Invoke(binding.boundary, "wails.deletion.preview_meeting", func(string) (DeletionPreviewDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return DeletionPreviewDTO{}, err
		}
		service, err := binding.service()
		if err != nil {
			return DeletionPreviewDTO{}, err
		}
		preview, err := service.PreviewMeeting(context.Background(), meetingID)
		return mapDeletionPreviewDTO(preview), err
	})
}

// DeleteMeeting 校验手工会议号并永久删除整场会议。
func (binding *DeletionBinding) DeleteMeeting(input DeleteMeetingDTO) Result[DeletionJobDTO] {
	return Invoke(binding.boundary, "wails.deletion.delete_meeting", func(string) (DeletionJobDTO, error) {
		if err := validateDeleteInput(input.MeetingID, input.Revision, input.Digest); err != nil || input.MeetingNo == "" {
			return DeletionJobDTO{}, fmt.Errorf("删除会议确认无效")
		}
		service, err := binding.service()
		if err != nil {
			return DeletionJobDTO{}, err
		}
		job, err := service.DeleteMeeting(context.Background(), input.MeetingID, input.MeetingNo, input.Revision, input.Digest)
		return mapDeletionJobDTO(job), err
	})
}

// GetDeletionJob 返回同场当前删除恢复状态。
func (binding *DeletionBinding) GetDeletionJob(meetingID string) Result[DeletionJobDTO] {
	return Invoke(binding.boundary, "wails.deletion.get_job", func(string) (DeletionJobDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return DeletionJobDTO{}, err
		}
		service, err := binding.service()
		if err != nil {
			return DeletionJobDTO{}, err
		}
		job, err := service.GetJobByMeeting(context.Background(), meetingID)
		if err != nil || job == nil {
			return DeletionJobDTO{}, err
		}
		return mapDeletionJobDTO(*job), nil
	})
}

// RetryDeletion 只处理原 manifest 中的剩余项目。
func (binding *DeletionBinding) RetryDeletion(jobID string) Result[DeletionJobDTO] {
	return Invoke(binding.boundary, "wails.deletion.retry", func(string) (DeletionJobDTO, error) {
		if err := requireUUID("job ID", jobID); err != nil {
			return DeletionJobDTO{}, err
		}
		service, err := binding.service()
		if err != nil {
			return DeletionJobDTO{}, err
		}
		job, err := service.Retry(context.Background(), jobID)
		return mapDeletionJobDTO(job), err
	})
}

func validateDeleteInput(meetingID string, revision int64, digest string) error {
	if err := requireUUID("meeting ID", meetingID); err != nil {
		return err
	}
	decoded, err := hex.DecodeString(digest)
	if revision < 0 || err != nil || len(decoded) != 32 || digest != strings.ToLower(digest) {
		return fmt.Errorf("删除预览参数无效")
	}
	return nil
}
