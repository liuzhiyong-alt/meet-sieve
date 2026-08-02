package transcript

import (
	"context"
	"sync"
	"testing"
	"time"

	"meet-sieve/internal/port"
)

// TestFinalProcessorAcceptsCapacityAndDrainsInOrder 验证 final 有界队列排空所有已接受事件且保持顺序。
func TestFinalProcessorAcceptsCapacityAndDrainsInOrder(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var persisted []string
	processor := NewFinalProcessor(FinalProcessorDependencies{
		Capacity: 2, PersistTimeout: time.Second,
		Persist: func(_ context.Context, event port.TranscriptionEvent) error {
			if event.ProviderResultID == "1" {
				close(started)
				<-release
			}
			mu.Lock()
			persisted = append(persisted, event.ProviderResultID)
			mu.Unlock()
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := processor.Start(ctx); err != nil {
		t.Fatalf("启动 final processor 失败：%v", err)
	}
	if !processor.TrySubmit(finalEvent("1")) {
		t.Fatal("第一条 final 应被接受")
	}
	<-started
	if !processor.TrySubmit(finalEvent("2")) || !processor.TrySubmit(finalEvent("3")) {
		t.Fatal("队列容量两项应被接受")
	}
	if processor.TrySubmit(finalEvent("4")) {
		t.Fatal("第 129 类越界 final 必须立即背压")
	}
	close(release)
	if err := processor.CloseAndWait(context.Background()); err != nil {
		t.Fatalf("排空 final processor 失败：%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(persisted) != 3 || persisted[0] != "1" || persisted[1] != "2" || persisted[2] != "3" {
		t.Fatalf("final 排空顺序错误：%v", persisted)
	}
}

// TestPartialProjectorKeepsRevisionAndLimitsPublishRate 验证 partial 只接受更高 revision 且最多每 100ms 发布一次。
func TestPartialProjectorKeepsRevisionAndLimitsPublishRate(t *testing.T) {
	now := time.Unix(0, 0)
	var published []port.TranscriptionEvent
	projector := NewPartialProjector(func() time.Time { return now }, func(event port.TranscriptionEvent) {
		published = append(published, event)
	})
	projector.Accept(partialEvent("same", 1, "一"))
	now = now.Add(50 * time.Millisecond)
	projector.Accept(partialEvent("same", 2, "二"))
	projector.Accept(partialEvent("same", 1, "旧"))
	now = now.Add(50 * time.Millisecond)
	projector.Accept(partialEvent("same", 3, "三"))
	if len(published) != 2 || published[0].Text != "一" || published[1].Text != "三" {
		t.Fatalf("partial revision/限频错误：%+v", published)
	}
	projector.Clear("same")
	if projector.Size() != 0 {
		t.Fatal("final 后必须清除 partial")
	}
}

// finalEvent 创建测试 final。
func finalEvent(id string) port.TranscriptionEvent {
	return port.TranscriptionEvent{Type: port.TranscriptionFinal, ProviderResultID: id, Text: "正文", StartSample: 0, EndSample: 1}
}

// partialEvent 创建测试 partial。
func partialEvent(id string, revision int64, value string) port.TranscriptionEvent {
	return port.TranscriptionEvent{Type: port.TranscriptionPartial, ResultID: id, ProviderResultID: id, Revision: revision, Text: value}
}
