package wails

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ContextProvider 返回当前 Wails 生命周期 context，避免 dialog binding 持有失效上下文。
type ContextProvider func() context.Context

// DirectoryDialogBinding 只封装系统目录选择；取消返回空字符串成功结果，绝不初始化工作目录。
type DirectoryDialogBinding struct {
	contextProvider ContextProvider
	boundary        *Boundary
}

// NewDirectoryDialogBinding 创建系统目录选择 binding。
func NewDirectoryDialogBinding(contextProvider ContextProvider, boundary *Boundary) *DirectoryDialogBinding {
	return &DirectoryDialogBinding{contextProvider: contextProvider, boundary: boundary}
}

// ChooseWorkspaceDirectory 打开系统目录选择器；调用方负责将返回路径交给 inspect/use/save。
func (binding *DirectoryDialogBinding) ChooseWorkspaceDirectory() Result[string] {
	return Invoke(binding.boundary, "wails.dialog.choose_workspace_directory", func(_ string) (string, error) {
		if binding == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
			return "", fmt.Errorf("目录选择器尚未准备")
		}
		return runtime.OpenDirectoryDialog(binding.contextProvider(), runtime.OpenDialogOptions{
			Title:                "选择会议工作目录",
			CanCreateDirectories: true,
		})
	})
}
