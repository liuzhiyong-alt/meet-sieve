package correction

import (
	"context"
	"fmt"

	correctionrepository "meet-sieve/internal/repository/correction"
)

// Entry 是校对工作台可恢复的安全当前投影。
type Entry struct {
	Seq                      int64
	UtteranceID              string
	StartSample              int64
	EndSample                int64
	OriginalText             string
	CurrentText              string
	SpeakerDisplay           string
	CurrentParticipantID     string
	SpeakerClusterID         string
	AssignmentSource         string
	TextRevision             int
	SpeakerRevision          int
	ClusterRevision          int
	ClusterCount             int
	CanPlay                  bool
	PlaybackDisabledReason   string
	CanEnroll                bool
	EnrollmentDisabledReason string
}

// Participant 是本场 speaker 校对选择项，不暴露全局成员库。
type Participant struct {
	ID          string
	DisplayName string
	Kind        string
	IsMember    bool
}

// Page 是按 seq 游标恢复的校对工作台快照。
type Page struct {
	Entries      []Entry
	Participants []Participant
	NextSeq      int64
}

// QueryService 从 SQLite 分页恢复校对页，不依赖 Wails event 或 Pinia 内存。
type QueryService struct {
	repository *correctionrepository.Repository
}

// NewQueryService 创建校对只读服务。
func NewQueryService(repository *correctionrepository.Repository) *QueryService {
	return &QueryService{repository: repository}
}

// ListEntries 默认 100、最多 200 条，并返回本场 participant 快照。
func (service *QueryService) ListEntries(ctx context.Context, meetingID string, afterSeq int64, limit int) (Page, error) {
	if service == nil || service.repository == nil || meetingID == "" || afterSeq < 0 {
		return Page{}, fmt.Errorf("correction query 参数无效")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := service.repository.ListEntries(ctx, meetingID, afterSeq, limit)
	if err != nil {
		return Page{}, err
	}
	participants, err := service.repository.ListParticipants(ctx, meetingID)
	if err != nil {
		return Page{}, err
	}
	page := Page{Entries: mapEntries(rows, participants), Participants: mapParticipants(participants)}
	if len(page.Entries) > 0 {
		page.NextSeq = page.Entries[len(page.Entries)-1].Seq
	}
	return page, nil
}

// GetEntry 按 utterance ID 从 SQLite 恢复单条详情。
func (service *QueryService) GetEntry(ctx context.Context, utteranceID string) (Entry, error) {
	if service == nil || service.repository == nil || utteranceID == "" {
		return Entry{}, fmt.Errorf("correction entry 参数无效")
	}
	meetingID, seq, err := service.repository.FindEntryCursor(ctx, utteranceID)
	if err != nil {
		return Entry{}, err
	}
	page, err := service.ListEntries(ctx, meetingID, seq-1, 1)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].UtteranceID != utteranceID {
		return Entry{}, fmt.Errorf("correction entry 不存在")
	}
	return page.Entries[0], nil
}

// mapEntries 计算安全 speaker display 与播放/声纹能力说明。
func mapEntries(rows []correctionrepository.EntryRow, participants []correctionrepository.ParticipantRow) []Entry {
	members := make(map[string]bool, len(participants))
	for _, participant := range participants {
		members[participant.ID] = participant.MemberID != ""
	}
	result := make([]Entry, 0, len(rows))
	for _, row := range rows {
		entry := Entry{
			Seq: row.Seq, UtteranceID: row.UtteranceID, StartSample: row.StartSample, EndSample: row.EndSample,
			OriginalText: row.OriginalText, CurrentText: row.CurrentText,
			SpeakerDisplay: row.ParticipantDisplayName, CurrentParticipantID: row.CurrentParticipantID,
			SpeakerClusterID: row.SpeakerClusterID, AssignmentSource: row.AssignmentSource,
			TextRevision: row.TextRevision, SpeakerRevision: row.SpeakerRevision,
			ClusterRevision: row.ClusterRevision, ClusterCount: row.ClusterCount, CanPlay: row.AudioReady,
		}
		if entry.SpeakerDisplay == "" && row.ClusterDisplayNo > 0 {
			entry.SpeakerDisplay = fmt.Sprintf("未知说话人 %d", row.ClusterDisplayNo)
		}
		if entry.SpeakerDisplay == "" {
			entry.SpeakerDisplay = "未知说话人"
		}
		if !entry.CanPlay {
			entry.PlaybackDisabledReason = "对应录音不可回放"
		}
		entry.CanEnroll = entry.CanPlay && members[row.CurrentParticipantID]
		if !entry.CanEnroll {
			entry.EnrollmentDisabledReason = "仅支持将可回放且已归属正式成员的片段加入声纹"
		}
		result = append(result, entry)
	}
	return result
}

// mapParticipants 转换本场 participant 最小选择投影。
func mapParticipants(rows []correctionrepository.ParticipantRow) []Participant {
	result := make([]Participant, 0, len(rows))
	for _, row := range rows {
		result = append(result, Participant{ID: row.ID, DisplayName: row.DisplayName, Kind: row.Kind, IsMember: row.MemberID != ""})
	}
	return result
}
