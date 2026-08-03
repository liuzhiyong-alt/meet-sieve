package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ownedPartName  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.part$`)
	ownedFinalName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}(\.[A-Za-z0-9]{1,16})?$`)
)

// ReferencedFileLoader 返回指定会议已入库的内部 safe_name 集合。
type ReferencedFileLoader func(context.Context, string) (map[string]struct{}, error)

// Recovery 只清理应用自有命名范围内的暂存和孤儿候选。
type Recovery struct {
	loadReferenced ReferencedFileLoader
}

// NewRecovery 创建附件崩溃恢复服务。
func NewRecovery(loader ReferencedFileLoader) *Recovery {
	return &Recovery{loadReferenced: loader}
}

// RecoverMeeting 删除 `.staging/*.part` 和未被 SQLite 引用的 UUID final candidate。
func (recovery *Recovery) RecoverMeeting(ctx context.Context, meetingID string, meetingDirectory string) error {
	if recovery == nil || recovery.loadReferenced == nil || meetingID == "" || !filepath.IsAbs(meetingDirectory) {
		return fmt.Errorf("附件恢复参数无效")
	}
	referenced, err := recovery.loadReferenced(ctx, meetingID)
	if err != nil {
		return fmt.Errorf("读取已入库附件：%w", err)
	}
	resourcesPath := filepath.Join(filepath.Clean(meetingDirectory), "resources")
	var recoveryErr error
	recoveryErr = errors.Join(recoveryErr, removeOwnedFiles(ctx, filepath.Join(resourcesPath, ".staging"), ownedPartName, nil))
	recoveryErr = errors.Join(recoveryErr, removeOwnedFiles(ctx, resourcesPath, ownedFinalName, referenced))
	return recoveryErr
}

// removeOwnedFiles 只扫描固定单层目录，不跟随 symlink 且不递归用户文件。
func removeOwnedFiles(ctx context.Context, directory string, pattern *regexp.Regexp, referenced map[string]struct{}) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("扫描附件恢复目录：%w", err)
	}
	var cleanupErr error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return errors.Join(cleanupErr, err)
		}
		name := entry.Name()
		if !pattern.MatchString(name) {
			continue
		}
		if _, keep := referenced[name]; keep {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("删除附件孤儿 %s：%w", name, err))
		}
	}
	return cleanupErr
}
