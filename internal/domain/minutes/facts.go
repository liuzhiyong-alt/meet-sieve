package minutes

// FactKind 区分纪要允许消费的事实种类。
type FactKind string

const (
	// FactUtterance 是当前校正投影后的正式转写。
	FactUtterance FactKind = "utterance"
	// FactMessage 是主持人或访客公开提交的会议消息。
	FactMessage FactKind = "message"
	// FactResource 是已完成附件或链接的安全索引。
	FactResource FactKind = "resource"
)

// Fact 是纪要 provider 可消费的严格白名单事实。
type Fact struct {
	Seq          int64    `json:"seq"`
	Kind         FactKind `json:"kind"`
	OccurredAt   int64    `json:"occurred_at"`
	StartSample  int64    `json:"start_sample,omitempty"`
	EndSample    int64    `json:"end_sample,omitempty"`
	Speaker      string   `json:"speaker,omitempty"`
	Text         string   `json:"text"`
	ResourceKind string   `json:"resource_kind,omitempty"`
	ResourceName string   `json:"resource_name,omitempty"`
	MediaType    string   `json:"media_type,omitempty"`
	SizeBytes    int64    `json:"size_bytes,omitempty"`
	Revision     int      `json:"revision"`
}

// MeetingSnapshot 是生成开始时冻结的会议基础事实。
type MeetingSnapshot struct {
	MeetingID    string   `json:"meeting_id"`
	MeetingNo    string   `json:"meeting_no"`
	Subject      string   `json:"subject"`
	StartedAt    int64    `json:"started_at"`
	EndedAt      int64    `json:"ended_at"`
	CutoffSeq    int64    `json:"cutoff_seq"`
	Timezone     string   `json:"timezone"`
	Participants []string `json:"participants"`
}

// Context 是一次纪要生成固定的全部白名单输入。
type Context struct {
	Meeting MeetingSnapshot `json:"meeting"`
	Facts   []Fact          `json:"facts"`
	Gaps    []GapNotice     `json:"gaps"`
}
