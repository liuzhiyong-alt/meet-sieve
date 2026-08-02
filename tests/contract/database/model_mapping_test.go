package database_test

import (
	"reflect"
	"strings"
	"testing"

	"meet-sieve/models"
)

// tableModel 是显式 GORM 表名映射的最小契约。
type tableModel interface {
	TableName() string
}

// TestModels_DeclareExplicitColumnMappings 验证所有持久化字段都声明稳定列名，避免依赖 GORM 的命名推断。
func TestModels_DeclareExplicitColumnMappings(t *testing.T) {
	modelsToCheck := []any{
		models.AppMetadata{}, models.Settings{}, models.Member{}, models.Group{}, models.GroupMember{},
		models.MeetingNumberSequence{}, models.Meeting{}, models.MeetingParticipant{},
		models.MeetingEvent{}, models.Utterance{}, models.GuestSession{}, models.Message{}, models.Resource{}, models.Correction{},
		models.AudioAsset{}, models.ASRSession{}, models.ASRGap{}, models.VoiceSample{}, models.VoiceEmbedding{}, models.SpeakerCluster{},
		models.AgentSession{}, models.AgentTurn{}, models.SyncBatch{}, models.ContextSnapshot{}, models.MinuteVersion{}, models.DeletionJob{},
	}
	for _, model := range modelsToCheck {
		modelType := reflect.TypeOf(model)
		for index := 0; index < modelType.NumField(); index++ {
			field := modelType.Field(index)
			mapping := field.Tag.Get("gorm")
			if !strings.HasPrefix(mapping, "column:") {
				t.Fatalf("%s.%s 必须显式声明 gorm column 映射，当前为 %q", modelType.Name(), field.Name, mapping)
			}
		}
	}
}

// TestModels_UseExplicitStep1TableNames 验证所有 Step 1 模型都声明数据库表名，不依赖默认复数推断。
func TestModels_UseExplicitStep1TableNames(t *testing.T) {
	tests := []struct {
		name  string
		model tableModel
	}{
		{"app_metadata", models.AppMetadata{}}, {"settings", models.Settings{}},
		{"members", models.Member{}}, {"groups", models.Group{}}, {"group_members", models.GroupMember{}},
		{"meeting_number_sequences", models.MeetingNumberSequence{}}, {"meetings", models.Meeting{}}, {"meeting_participants", models.MeetingParticipant{}},
		{"meeting_events", models.MeetingEvent{}}, {"utterances", models.Utterance{}}, {"guest_sessions", models.GuestSession{}}, {"messages", models.Message{}}, {"resources", models.Resource{}}, {"corrections", models.Correction{}},
		{"audio_assets", models.AudioAsset{}}, {"asr_sessions", models.ASRSession{}}, {"asr_gaps", models.ASRGap{}}, {"voice_samples", models.VoiceSample{}}, {"voice_embeddings", models.VoiceEmbedding{}}, {"speaker_clusters", models.SpeakerCluster{}},
		{"agent_sessions", models.AgentSession{}}, {"agent_turns", models.AgentTurn{}}, {"sync_batches", models.SyncBatch{}}, {"context_snapshots", models.ContextSnapshot{}}, {"minute_versions", models.MinuteVersion{}}, {"deletion_jobs", models.DeletionJob{}},
	}
	for _, test := range tests {
		if got := test.model.TableName(); got != test.name {
			t.Fatalf("模型表名不正确：got %q want %q", got, test.name)
		}
	}
}
