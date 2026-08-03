package agent_test

import (
	"context"
	"strings"
	"testing"

	serviceagent "meet-sieve/internal/service/agent"
)

// TestContextBuilder_FixesCutoffFiltersAndAdvancesUnknown 验证固定 cutoff、显式投影与未知事件推进。
func TestContextBuilder_FixesCutoffFiltersAndAdvancesUnknown(t *testing.T) {
	repository := &contextRepository{events: []serviceagent.ContextEvent{
		{Seq: 1, Kind: "utterance.final", Source: "asr", Text: "第一句话"},
		{Seq: 2, Kind: "future.kind", Source: "system"},
		{Seq: 3, Kind: "resource.created", Source: "guest", ResourceKind: "attachment", ResourceState: "uploading"},
		{Seq: 4, Kind: "message.created", Source: "guest", DisplayName: "访客", Text: "补充材料"},
		{Seq: 5, Kind: "ai.question", Source: "host", Text: "当前问题"},
		{Seq: 6, Kind: "message.created", Source: "guest", Text: "本轮之后"},
	}}
	builder := serviceagent.NewContextBuilder(repository)
	result, err := builder.Build(context.Background(), serviceagent.BuildContextRequest{
		MeetingID: "meeting", SessionID: "session", ThroughSeq: 0, CutoffSeq: 5,
		Purpose: "answer", Question: "当前问题",
	})
	if err != nil {
		t.Fatalf("构建上下文失败：%v", err)
	}
	if result.ThroughSeq != 5 || len(result.Batches) != 1 {
		t.Fatalf("游标或批次错误：%#v", result)
	}
	content := string(result.Batches[0].Content)
	if !strings.Contains(content, "第一句话") || !strings.Contains(content, "补充材料") || strings.Contains(content, "future.kind") || strings.Contains(content, "uploading") || strings.Contains(content, "本轮之后") {
		t.Fatalf("上下文投影错误：%s", content)
	}
	if !strings.Contains(result.FinalPrompt, "本次问题\n当前问题") {
		t.Fatalf("当前问题没有单列：%s", result.FinalPrompt)
	}
}

// TestContextBuilder_SplitsAtCountOrBytesAndBuildsStableKeys 验证 200 条/64KiB 边界和幂等键。
func TestContextBuilder_SplitsAtCountOrBytesAndBuildsStableKeys(t *testing.T) {
	events := make([]serviceagent.ContextEvent, 0, 201)
	for sequence := int64(1); sequence <= 201; sequence++ {
		events = append(events, serviceagent.ContextEvent{Seq: sequence, Kind: "message.created", Source: "guest", DisplayName: "访客", Text: "内容"})
	}
	builder := serviceagent.NewContextBuilder(&contextRepository{events: events})
	result, err := builder.Build(context.Background(), serviceagent.BuildContextRequest{
		MeetingID: "meeting", SessionID: "session", ThroughSeq: 0, CutoffSeq: 201, Purpose: "answer", Question: "总结",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Batches) != 2 || result.Batches[0].FromSeq != 1 || result.Batches[0].ToSeq != 200 || result.Batches[1].FromSeq != 201 || result.Batches[1].ToSeq != 201 {
		t.Fatalf("计数切批错误：%#v", result.Batches)
	}
	if len(result.Batches[0].IdempotencyKey) != 64 || result.Batches[0].IdempotencyKey == result.Batches[1].IdempotencyKey {
		t.Fatalf("批次幂等键错误：%#v", result.Batches)
	}
}

type contextRepository struct{ events []serviceagent.ContextEvent }

// ListContextEvents 返回测试固定事件并模拟 cutoff SQL 过滤。
func (repository *contextRepository) ListContextEvents(_ context.Context, _ string, afterSeq int64, cutoffSeq int64) ([]serviceagent.ContextEvent, error) {
	result := make([]serviceagent.ContextEvent, 0)
	for _, event := range repository.events {
		if event.Seq > afterSeq && event.Seq <= cutoffSeq {
			result = append(result, event)
		}
	}
	return result, nil
}
