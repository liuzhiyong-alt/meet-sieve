package content

import (
	"testing"

	contentrepository "meet-sieve/internal/repository/content"
)

// TestSpeakerIdentity_HidesProviderTrackLabel 验证供应商轨道标签不会被展示成识别结果。
func TestSpeakerIdentity_HidesProviderTrackLabel(t *testing.T) {
	key, label := speakerIdentity(contentrepository.TimelineRow{
		SpeakerTrackID: "track-1", TrackDisplayNo: 2, ASRSessionID: "session-1", ASRSpeakerLabel: "speaker_0",
	})
	if key != "track:track-1" {
		t.Fatalf("稳定说话人键错误：%s", key)
	}
	if label != "说话人 2" {
		t.Fatalf("供应商标签不应直接展示：%s", label)
	}
}

// TestSpeakerIdentity_IsolatesFinalWithoutProviderSpeaker 验证无标签片段不会被错误合并成同一 speaker。
func TestSpeakerIdentity_IsolatesFinalWithoutProviderSpeaker(t *testing.T) {
	key, label := speakerIdentity(contentrepository.TimelineRow{UtteranceID: "utterance-1"})
	if key != "unlabeled:utterance-1" || label != "未识别说话人" {
		t.Fatalf("无标签说话人映射错误：key=%s label=%s", key, label)
	}
}

// TestSpeakerIdentity_PrefersParticipantName 验证正式成员关联后仍展示成员姓名。
func TestSpeakerIdentity_PrefersParticipantName(t *testing.T) {
	key, label := speakerIdentity(contentrepository.TimelineRow{
		ParticipantID: "participant-1", ParticipantName: "成员甲", ASRSpeakerLabel: "speaker_0",
	})
	if key != "participant:participant-1" || label != "成员甲" {
		t.Fatalf("正式成员身份映射错误：key=%s label=%s", key, label)
	}
}

// TestProjectTimelineRow_IncludesSpeakerRevision 验证前端可按修订号拒绝旧说话人投影。
func TestProjectTimelineRow_IncludesSpeakerRevision(t *testing.T) {
	entry, visible := projectTimelineRow(contentrepository.TimelineRow{
		Seq: 1, EventKind: "utterance.final", UtteranceID: "utterance-1", SpeakerRevision: 3,
	})
	if !visible || entry.SpeakerRevision != 3 {
		t.Fatalf("说话人修订号投影错误：visible=%v entry=%+v", visible, entry)
	}
}

// TestQuestionSpeakerIdentity_PrefersCurrentTriggerProjection 验证 AI 问题随触发 utterance 的后续校对更新展示身份。
func TestQuestionSpeakerIdentity_PrefersCurrentTriggerProjection(t *testing.T) {
	key, label := questionSpeakerIdentity(contentrepository.TimelineRow{
		AgentTriggerID: "utterance-1", AgentParticipantID: "participant-1", AgentParticipantName: "成员甲",
	}, agentPayload{Version: 3, SpeakerKeySnapshot: "cluster:old", SpeakerLabelSnapshot: "未知说话人 1"})
	if key != "participant:participant-1" || label != "成员甲" {
		t.Fatalf("AI 问题必须优先当前投影：key=%s label=%s", key, label)
	}
}

// TestQuestionSpeakerIdentity_FallsBackToPayloadSnapshot 验证历史或已删除触发 utterance 仍保留创建时审计快照。
func TestQuestionSpeakerIdentity_FallsBackToPayloadSnapshot(t *testing.T) {
	key, label := questionSpeakerIdentity(contentrepository.TimelineRow{}, agentPayload{
		Version: 3, SpeakerKeySnapshot: "track:track-1", SpeakerLabelSnapshot: "说话人 1",
	})
	if key != "track:track-1" || label != "说话人 1" {
		t.Fatalf("AI 问题快照回退错误：key=%s label=%s", key, label)
	}
}
