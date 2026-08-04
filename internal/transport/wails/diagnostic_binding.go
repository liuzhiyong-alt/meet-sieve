package wails

import (
	"context"
	"fmt"
	"path/filepath"

	"meet-sieve/internal/adapter/systemopen"
	"meet-sieve/internal/infra/filesystem"
	diagnosticsservice "meet-sieve/internal/service/diagnostics"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// DiagnosticServiceProvider 返回当前工作目录的扫描和导出服务。
type DiagnosticServiceProvider func() (*diagnosticsservice.StorageScanService, *diagnosticsservice.ExportService, error)

// DiagnosticBinding 编排存储扫描、系统保存对话框和日志目录打开。
type DiagnosticBinding struct {
	service         DiagnosticServiceProvider
	contextProvider ContextProvider
	boundary        *Boundary
}

// NewDiagnosticBinding 创建诊断 Binding。
func NewDiagnosticBinding(service DiagnosticServiceProvider, contextProvider ContextProvider, boundary *Boundary) *DiagnosticBinding {
	return &DiagnosticBinding{service: service, contextProvider: contextProvider, boundary: boundary}
}

// StartStorageScan 启动真实只读扫描。
func (binding *DiagnosticBinding) StartStorageScan() Result[StorageScanDTO] {
	return Invoke(binding.boundary, "wails.diagnostics.storage.start", func(string) (StorageScanDTO, error) {
		scanner, _, err := binding.service()
		if err != nil {
			return StorageScanDTO{}, err
		}
		snapshot, err := scanner.Start(context.Background())
		return mapStorageScanDTO(snapshot), err
	})
}

// GetStorageScan 返回当前阶段或上次结果。
func (binding *DiagnosticBinding) GetStorageScan() Result[StorageScanDTO] {
	return Invoke(binding.boundary, "wails.diagnostics.storage.get", func(string) (StorageScanDTO, error) {
		scanner, _, err := binding.service()
		if err != nil {
			return StorageScanDTO{}, err
		}
		return mapStorageScanDTO(scanner.Get()), nil
	})
}

// ExportGlobalDiagnostic 通过系统保存对话框选择目标后导出全局诊断。
func (binding *DiagnosticBinding) ExportGlobalDiagnostic() Result[DiagnosticExportDTO] {
	return binding.exportDiagnostic("")
}

// ExportMeetingDiagnostic 通过系统保存对话框导出本场白名单摘要。
func (binding *DiagnosticBinding) ExportMeetingDiagnostic(meetingID string) Result[DiagnosticExportDTO] {
	return binding.exportDiagnostic(meetingID)
}

// exportDiagnostic 统一处理系统对话框取消和安全目标扩展名。
func (binding *DiagnosticBinding) exportDiagnostic(meetingID string) Result[DiagnosticExportDTO] {
	return Invoke(binding.boundary, "wails.diagnostics.export", func(string) (DiagnosticExportDTO, error) {
		if meetingID != "" {
			if err := requireUUID("meeting ID", meetingID); err != nil {
				return DiagnosticExportDTO{}, err
			}
		}
		if binding.contextProvider == nil || binding.contextProvider() == nil {
			return DiagnosticExportDTO{}, fmt.Errorf("保存对话框尚未准备")
		}
		target, err := runtime.SaveFileDialog(binding.contextProvider(), runtime.SaveDialogOptions{
			Title: "导出 MeetSieve 诊断", DefaultFilename: "meetsieve-diagnostic.zip",
			Filters: []runtime.FileFilter{{DisplayName: "ZIP 诊断包", Pattern: "*.zip"}},
		})
		if err != nil {
			return DiagnosticExportDTO{}, err
		}
		if target == "" {
			return DiagnosticExportDTO{Cancelled: true}, nil
		}
		if filepath.Ext(target) == "" {
			target += ".zip"
		}
		_, exporter, err := binding.service()
		if err != nil {
			return DiagnosticExportDTO{}, err
		}
		var result diagnosticsservice.ExportResult
		if meetingID == "" {
			result, err = exporter.ExportGlobal(context.Background(), target)
		} else {
			result, err = exporter.ExportMeeting(context.Background(), meetingID, target)
		}
		return mapDiagnosticExportDTO(result), err
	})
}

// OpenLogDirectory 仅在用户点击后打开受控日志目录。
func (binding *DiagnosticBinding) OpenLogDirectory() Result[CommandResultDTO] {
	return Invoke(binding.boundary, "wails.diagnostics.open_logs", func(string) (CommandResultDTO, error) {
		logRoot, err := filesystem.CurrentLogDir()
		if err != nil {
			return CommandResultDTO{}, err
		}
		if err := systemopen.NewLauncher().Open(context.Background(), logRoot); err != nil {
			return CommandResultDTO{}, err
		}
		return CommandResultDTO{Executed: true}, nil
	})
}
