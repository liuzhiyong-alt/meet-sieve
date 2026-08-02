package filesystem

import "strings"

// VolumeKind 表示工作目录所在卷能否被安全地用于本地 SQLite。
type VolumeKind string

const (
	// VolumeLocal 表示已识别的本地内置或外接卷。
	VolumeLocal VolumeKind = "local"
	// VolumeNetwork 表示 SMB、NFS 或 WebDAV 等网络共享卷。
	VolumeNetwork VolumeKind = "network"
	// VolumeUnknown 表示无法可信判断的卷，调用方必须拒绝。
	VolumeUnknown VolumeKind = "unknown"
)

// ClassifyFilesystemType 按文件系统类型分类；未登记类型保持 unknown 以 fail closed。
func ClassifyFilesystemType(filesystemType string) VolumeKind {
	switch strings.ToLower(strings.TrimSpace(filesystemType)) {
	case "apfs", "hfs", "hfs+", "exfat", "msdos", "fat", "ntfs":
		return VolumeLocal
	case "smbfs", "smb", "nfs", "webdav", "webdavfs":
		return VolumeNetwork
	default:
		return VolumeUnknown
	}
}
