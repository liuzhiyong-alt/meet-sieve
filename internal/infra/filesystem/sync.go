package filesystem

// SyncDirectory 刷新目录项，确保同目录 rename 在支持的平台上持久化。
func SyncDirectory(path string) error {
	return syncDirectory(path)
}
