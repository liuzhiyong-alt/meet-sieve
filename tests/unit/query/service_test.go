package query_test

import (
	"context"
	"testing"

	querydomain "meet-sieve/internal/domain/query"
	"meet-sieve/internal/infra/apperr"
	queryrepository "meet-sieve/internal/repository/query"
	queryservice "meet-sieve/internal/service/query"
)

// TestService_ListMeetingsSignsCursorsAndMapsChangedFilter 验证服务持有游标签名与稳定错误语义。
func TestService_ListMeetingsSignsCursorsAndMapsChangedFilter(t *testing.T) {
	repository := &queryRepositoryStub{page: queryrepository.MeetingPage{HasMore: true, Items: []queryrepository.MeetingSummaryRow{
		{ID: "1", MeetingNo: "M-02", StartedAt: 2000},
		{ID: "2", MeetingNo: "M-01", StartedAt: 1000},
	}}}
	service := queryservice.NewService(repository)
	page, err := service.ListMeetings(context.Background(), queryservice.ListMeetingsInput{Limit: 2})
	if err != nil || page.NextCursor == "" || page.PreviousCursor != "" {
		t.Fatalf("第一页游标错误：page=%+v err=%v", page, err)
	}

	repository.page.HasMore = false
	_, err = service.ListMeetings(context.Background(), queryservice.ListMeetingsInput{
		Search: "changed", Cursor: page.NextCursor, Limit: 2,
	})
	if err == nil || apperr.Normalize(err).ErrorCode != apperr.CodeQueryCursorFilterChanged.ErrorCode {
		t.Fatalf("筛选变化错误映射错误：%v", err)
	}
}

type queryRepositoryStub struct {
	page            queryrepository.MeetingPage
	meeting         *queryrepository.MeetingSummaryRow
	facts           queryrepository.RecoveryFactsRow
	transcriptRows  []queryrepository.TranscriptRow
	transcriptState queryrepository.SeqPageState
}

// ListMeetings 返回测试指定的只读页。
func (stub *queryRepositoryStub) ListMeetings(_ context.Context, _ queryrepository.ListInput) (queryrepository.MeetingPage, error) {
	return stub.page, nil
}

// FindHighestPriorityMeeting 返回测试指定的首页续办项。
func (stub *queryRepositoryStub) FindHighestPriorityMeeting(_ context.Context) (*queryrepository.MeetingSummaryRow, error) {
	return stub.meeting, nil
}

// GetMeeting 返回空详情，本测试不使用。
func (stub *queryRepositoryStub) GetMeeting(_ context.Context, _ string) (*queryrepository.MeetingSummaryRow, error) {
	return stub.meeting, nil
}

// ListTranscript 返回测试指定的原始记录与页边界。
func (stub *queryRepositoryStub) ListTranscript(_ context.Context, _ string, _ int64, _ int64, _ int) ([]queryrepository.TranscriptRow, queryrepository.SeqPageState, error) {
	return stub.transcriptRows, stub.transcriptState, nil
}

// ListContent 返回空页，本测试不使用。
func (stub *queryRepositoryStub) ListContent(_ context.Context, _ string, _ int64, _ int64, _ int) ([]queryrepository.ContentRow, queryrepository.SeqPageState, error) {
	return nil, queryrepository.SeqPageState{}, nil
}

// CountStatus 返回零，本测试不使用。
func (stub *queryRepositoryStub) CountStatus(_ context.Context, _ querydomain.MeetingStatus) (int, error) {
	return 0, nil
}

// GetRecoveryFacts 返回空恢复事实，本测试不使用。
func (stub *queryRepositoryStub) GetRecoveryFacts(_ context.Context, _ string) (queryrepository.RecoveryFactsRow, error) {
	return stub.facts, nil
}

// TestService_GetInterruptedRecoveryReturnsFileAndGapFacts 验证恢复页不依赖旧进程事件。
func TestService_GetInterruptedRecoveryReturnsFileAndGapFacts(t *testing.T) {
	stub := &queryRepositoryStub{
		meeting: &queryrepository.MeetingSummaryRow{ID: "meeting", LifecycleState: "interrupted", LocalSaveState: "failed"},
		facts:   queryrepository.RecoveryFactsRow{SegmentCount: 3, DurationSamples: 48000, SampleRate: 16000, FirstSequence: 1, LastSequence: 3, GapCount: 2, PendingGapCount: 1, ReadyFileCount: 3, FailureStage: "TAIL_TIMEOUT"},
	}
	recovery, err := queryservice.NewService(stub).GetInterruptedRecovery(context.Background(), "meeting")
	if err != nil || !recovery.CanRetry || recovery.Facts.DurationSamples != 48000 || recovery.Facts.PendingGapCount != 1 {
		t.Fatalf("恢复事实错误：recovery=%+v err=%v", recovery, err)
	}
}

// TestPrimaryActionForProjectsOneStableAction 验证最高状态只映射一个不含前端路由的稳定主动作。
func TestPrimaryActionForProjectsOneStableAction(t *testing.T) {
	tests := []struct {
		name       string
		status     querydomain.MeetingStatus
		gapID      string
		wantKind   string
		wantTarget string
	}{
		{name: "删除恢复", status: querydomain.StatusDeleting, wantKind: "deletion_recovery", wantTarget: "meeting-1"},
		{name: "保存恢复", status: querydomain.StatusRecoveryRequired, wantKind: "recover_meeting", wantTarget: "meeting-1"},
		{name: "缺口冲突", status: querydomain.StatusGapConflict, gapID: "gap-1", wantKind: "resolve_gap", wantTarget: "gap-1"},
		{name: "补转写", status: querydomain.StatusGapPending, gapID: "gap-2", wantKind: "open_gap", wantTarget: "gap-2"},
		{name: "纪要已生成", status: querydomain.StatusMinuteCandidate, wantKind: "open_meeting", wantTarget: "meeting-1"},
		{name: "Codex 未同步", status: querydomain.StatusAgentUnsynced, wantKind: "open_meeting", wantTarget: "meeting-1"},
		{name: "已保存", status: querydomain.StatusSaved, wantKind: "open_meeting", wantTarget: "meeting-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action := queryservice.PrimaryActionFor(queryrepository.MeetingSummaryRow{
				ID: "meeting-1", HighestStatus: test.status, PendingGapID: test.gapID,
			})
			if action.Kind != test.wantKind || action.TargetID != test.wantTarget || !action.Enabled {
				t.Fatalf("主动作投影错误：%+v", action)
			}
		})
	}
}

// TestService_ListTranscriptProjectsSafeSpeakerLabels 验证详情页不会暴露 ASR 或事件内部标识。
func TestService_ListTranscriptProjectsSafeSpeakerLabels(t *testing.T) {
	stub := &queryRepositoryStub{
		transcriptRows: []queryrepository.TranscriptRow{
			{Seq: 1, Kind: "utterance.final", SpeakerName: "张三"},
			{Seq: 2, Kind: "utterance.final", ClusterDisplayNo: 2},
			{Seq: 3, Kind: "utterance.final", TrackDisplayNo: 3},
			{Seq: 4, Kind: "utterance.final"},
			{Seq: 5, Kind: "ai.answer"},
		},
		transcriptState: queryrepository.SeqPageState{HasPrevious: true, HasNext: true},
	}

	page, err := queryservice.NewService(stub).ListTranscript(context.Background(), queryservice.SeqPageInput{MeetingID: "meeting"})
	if err != nil {
		t.Fatalf("读取原始记录失败：%v", err)
	}
	want := []string{"张三", "未知说话人 2", "说话人 3", "未识别说话人", ""}
	for index, item := range page.Items {
		if item.SpeakerDisplay != want[index] {
			t.Fatalf("第 %d 条说话人展示错误：got=%q want=%q", index+1, item.SpeakerDisplay, want[index])
		}
	}
	if !page.HasPrevious || !page.HasNext {
		t.Fatalf("分页方向未透传：%+v", page)
	}
}
