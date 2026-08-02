package port

import (
	"context"
	"io/fs"
)

// WorkspaceFile 保存工作目录内文件的内容和元信息。
type WorkspaceFile struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
}

// WorkspaceStore 定义受控工作目录内的最小文件读写边界。
// 具体目录初始化和迁移属于 Step 1，本接口不隐式创建工作目录。
type WorkspaceStore interface {
	Read(ctx context.Context, relativePath string) (WorkspaceFile, error)
	WriteAtomic(ctx context.Context, file WorkspaceFile) error
}
