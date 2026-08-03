package agent

// ContextEvent 是从 SQLite 投影出的单条安全会议上下文事实。
type ContextEvent struct {
	Seq           int64
	Kind          string
	OccurredAt    int64
	Source        string
	DisplayName   string
	Text          string
	URL           string
	RelativePath  string
	ResourceKind  string
	ResourceState string
	SizeBytes     int64
	SHA256        string
	GapReason     string
}
