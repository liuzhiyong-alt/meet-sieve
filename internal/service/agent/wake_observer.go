package agent

import (
	"context"
	"fmt"

	domainagent "meet-sieve/internal/domain/agent"
	agentrepository "meet-sieve/internal/repository/agent"
)

// WakeAskService 是 WakeObserver 唯一允许触发的主持人问答入口。
type WakeAskService interface {
	Ask(ctx context.Context, input AskInput) (AskResult, error)
}

// WakeObserver 只观察已提交 ASR final，并把匹配结果交给 TurnService。
type WakeObserver struct {
	repository *agentrepository.Repository
	turns      WakeAskService
}

// NewWakeObserver 创建 final 唤醒观察器。
func NewWakeObserver(repository *agentrepository.Repository, turns WakeAskService) *WakeObserver {
	return &WakeObserver{repository: repository, turns: turns}
}

// Observe 读取持久 final 并异步提交句首问题；忙或不可用不排队。
func (observer *WakeObserver) Observe(ctx context.Context, utteranceID string) (bool, error) {
	if observer == nil || observer.repository == nil || observer.turns == nil || utteranceID == "" {
		return false, fmt.Errorf("唤醒观察器未初始化")
	}
	final, err := observer.repository.GetWakeFinal(ctx, utteranceID)
	if err != nil {
		return false, err
	}
	settings, err := observer.repository.GetSettings(ctx)
	if err != nil {
		return false, err
	}
	wake, err := domainagent.NormalizeWakeWord(settings.WakeWord)
	if err != nil {
		return false, err
	}
	question := domainagent.NewWakeMatcher(wake).Match(final.Text)
	if question == "" {
		return false, nil
	}
	go func() {
		_, _ = observer.turns.Ask(context.Background(), AskInput{
			MeetingID: final.MeetingID, Question: question, Trigger: "wake_word",
			TriggerUtteranceID: &final.UtteranceID,
			IdempotencyKey:     "wake:" + final.UtteranceID + ":" + wake.Hash,
		})
	}()
	return true, nil
}
