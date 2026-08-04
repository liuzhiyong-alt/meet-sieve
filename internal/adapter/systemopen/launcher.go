// Package systemopen 把已由业务层验证的文件和 URL 交给操作系统默认程序。
package systemopen

import "context"

// Launcher 是资源打开服务消费的最小系统调用边界。
type Launcher struct{}

// NewLauncher 创建当前平台 launcher。
func NewLauncher() *Launcher { return &Launcher{} }

// Open 使用默认应用打开普通文件。
func (launcher *Launcher) Open(ctx context.Context, path string) error { return openFile(ctx, path) }

// Reveal 在系统文件管理器中定位普通文件。
func (launcher *Launcher) Reveal(ctx context.Context, path string) error {
	return revealFile(ctx, path)
}

// OpenURL 使用默认浏览器打开已验证的 HTTP(S) URL。
func (launcher *Launcher) OpenURL(ctx context.Context, target string) error {
	return openURL(ctx, target)
}
