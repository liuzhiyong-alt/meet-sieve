package wails

import (
	deletionservice "meet-sieve/internal/service/deletion"
	diagnosticsservice "meet-sieve/internal/service/diagnostics"
	resourceopenservice "meet-sieve/internal/service/resourceopen"
)

// DeletionPreviewDTO 是危险确认弹窗的不含路径摘要。
type DeletionPreviewDTO struct {
	MeetingID      string `json:"meeting_id"`
	MeetingNo      string `json:"meeting_no"`
	Kind           string `json:"kind"`
	Revision       int64  `json:"revision"`
	Digest         string `json:"digest"`
	FileCount      int    `json:"file_count"`
	DirectoryCount int    `json:"directory_count"`
	SymlinkCount   int    `json:"symlink_count"`
	UnknownCount   int    `json:"unknown_count"`
	SizeBytes      int64  `json:"size_bytes"`
}

// DeleteRecordingDTO 只提交会议和预览事实，不接受路径列表。
type DeleteRecordingDTO struct {
	MeetingID string `json:"meeting_id"`
	Revision  int64  `json:"revision"`
	Digest    string `json:"digest"`
}

// DeleteMeetingDTO 额外包含用户手工输入的会议号。
type DeleteMeetingDTO struct {
	MeetingID string `json:"meeting_id"`
	MeetingNo string `json:"meeting_no"`
	Revision  int64  `json:"revision"`
	Digest    string `json:"digest"`
}

// DeletionFailedItemDTO 是可展示的剩余删除项目。
type DeletionFailedItemDTO struct {
	ItemID   string `json:"item_id"`
	SafeName string `json:"safe_name"`
	Code     string `json:"code"`
}

// DeletionJobDTO 是删除恢复页的安全任务投影。
type DeletionJobDTO struct {
	JobID         string                  `json:"job_id"`
	MeetingID     string                  `json:"meeting_id"`
	Kind          string                  `json:"kind"`
	State         string                  `json:"state"`
	Remaining     []DeletionFailedItemDTO `json:"remaining"`
	AttemptCount  int                     `json:"attempt_count"`
	LastErrorCode string                  `json:"last_error_code,omitempty"`
}

// StorageScanDTO 是设置页存储扫描阶段和真实汇总。
type StorageScanDTO struct {
	Stage          string                               `json:"stage"`
	Running        bool                                 `json:"running"`
	ScannedItems   int64                                `json:"scanned_items"`
	TotalBytes     uint64                               `json:"total_bytes"`
	AvailableBytes uint64                               `json:"available_bytes"`
	WorkspaceBytes int64                                `json:"workspace_bytes"`
	Categories     diagnosticsservice.StorageCategories `json:"categories"`
	TopMeetings    []diagnosticsservice.MeetingStorage  `json:"top_meetings"`
	Warnings       []string                             `json:"warnings"`
	ScannedAt      int64                                `json:"scanned_at"`
	ErrorCode      string                               `json:"error_code,omitempty"`
}

// DiagnosticExportDTO 不返回保存绝对路径。
type DiagnosticExportDTO struct {
	FileName  string   `json:"file_name"`
	SizeBytes int64    `json:"size_bytes"`
	SHA256    string   `json:"sha256"`
	Entries   []string `json:"entries"`
	Cancelled bool     `json:"cancelled"`
}

// ResourceOpenDTO 是完整性状态和系统调用结果。
type ResourceOpenDTO struct {
	ResourceID     string `json:"resource_id"`
	IntegrityState string `json:"integrity_state,omitempty"`
	VerifiedAt     int64  `json:"verified_at,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
	Opened         bool   `json:"opened"`
}

// CommandResultDTO 表示系统命令是否已由用户明确触发。
type CommandResultDTO struct {
	Executed bool `json:"executed"`
}

func mapDeletionPreviewDTO(preview deletionservice.Preview) DeletionPreviewDTO {
	return DeletionPreviewDTO{MeetingID: preview.MeetingID, MeetingNo: preview.MeetingNo, Kind: string(preview.Kind), Revision: preview.Revision,
		Digest: preview.Digest, FileCount: preview.FileCount, DirectoryCount: preview.DirectoryCount, SymlinkCount: preview.SymlinkCount,
		UnknownCount: preview.UnknownCount, SizeBytes: preview.SizeBytes}
}

func mapDeletionJobDTO(job deletionservice.Result) DeletionJobDTO {
	remaining := make([]DeletionFailedItemDTO, 0, len(job.Remaining))
	for _, item := range job.Remaining {
		remaining = append(remaining, DeletionFailedItemDTO{ItemID: item.ItemID, SafeName: item.SafeName, Code: item.Code})
	}
	return DeletionJobDTO{JobID: job.JobID, MeetingID: job.MeetingID, Kind: string(job.Kind), State: job.State, Remaining: remaining, AttemptCount: job.AttemptCount, LastErrorCode: job.LastErrorCode}
}

func mapStorageScanDTO(snapshot diagnosticsservice.StorageSnapshot) StorageScanDTO {
	return StorageScanDTO{Stage: string(snapshot.Stage), Running: snapshot.Running, ScannedItems: snapshot.ScannedItems,
		TotalBytes: snapshot.TotalBytes, AvailableBytes: snapshot.AvailableBytes, WorkspaceBytes: snapshot.WorkspaceBytes,
		Categories: snapshot.Categories, TopMeetings: snapshot.TopMeetings, Warnings: snapshot.Warnings, ScannedAt: snapshot.ScannedAt, ErrorCode: snapshot.ErrorCode}
}

func mapDiagnosticExportDTO(result diagnosticsservice.ExportResult) DiagnosticExportDTO {
	return DiagnosticExportDTO{FileName: result.FileName, SizeBytes: result.SizeBytes, SHA256: result.SHA256, Entries: result.Entries}
}

func mapResourceOpenDTO(result resourceopenservice.Result) ResourceOpenDTO {
	return ResourceOpenDTO{ResourceID: result.ResourceID, IntegrityState: result.IntegrityState, VerifiedAt: result.VerifiedAt, Hostname: result.Hostname, Opened: result.Opened}
}
