package correction

import (
	"context"
	"fmt"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/repository/correction"
	speakerservice "meet-sieve/internal/service/speaker"
	voiceservice "meet-sieve/internal/service/voice"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MeetingVoiceEnrollment 定义复用 Step 2 永久声纹流水线的边界。
type MeetingVoiceEnrollment interface {
	PrepareMeetingPCM(ctx context.Context, requestID string, memberID string, meetingID string, utteranceID string, environmentKind string, samples []int16) (voiceservice.VoiceSample, error)
}

// MeetingClipDependencies 描述原始会议片段提取与永久 enrollment 依赖。
type MeetingClipDependencies struct {
	Repository   *correction.Repository
	Transactions *database.TransactionManager
	Audio        speakerservice.EvidenceAudioReader
	Enrollment   MeetingVoiceEnrollment
}

// MeetingClipCommand 要求独立 request ID 和显式二次确认。
type MeetingClipCommand struct {
	RequestID       string
	MeetingID       string
	UtteranceID     string
	EnvironmentKind string
	Confirmed       bool
}

// MeetingClipService 从原录音精确提取 utterance，不复用临时回放 clip。
type MeetingClipService struct {
	repository   *correction.Repository
	transactions *database.TransactionManager
	audio        speakerservice.EvidenceAudioReader
	enrollment   MeetingVoiceEnrollment
}

// NewMeetingClipService 创建会议片段永久声纹服务。
func NewMeetingClipService(dependencies MeetingClipDependencies) *MeetingClipService {
	return &MeetingClipService{repository: dependencies.Repository, transactions: dependencies.Transactions, audio: dependencies.Audio, enrollment: dependencies.Enrollment}
}

// Add 二次确认后读取原始采样范围，并交给 Step 2 同一质量/文件/embedding 流水线。
func (service *MeetingClipService) Add(ctx context.Context, command MeetingClipCommand) (voiceservice.VoiceSample, error) {
	if service == nil || service.repository == nil || service.transactions == nil || service.audio == nil || service.enrollment == nil || !command.Confirmed {
		return voiceservice.VoiceSample{}, apperr.Biz(apperr.CodeVoiceMeetingClipRejected, apperr.WithOp("correction.meeting_clip.confirm"))
	}
	for _, value := range []string{command.RequestID, command.MeetingID, command.UtteranceID} {
		if _, err := uuid.Parse(value); err != nil {
			return voiceservice.VoiceSample{}, apperr.Biz(apperr.CodeVoiceMeetingClipRejected, apperr.WithOp("correction.meeting_clip.validate"))
		}
	}
	var fact correction.MeetingClipEnrollmentFact
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var err error
		fact, err = service.repository.GetMeetingClipEnrollmentFact(ctx, tx, command.MeetingID, command.UtteranceID)
		return err
	})
	if err != nil {
		return voiceservice.VoiceSample{}, apperr.Biz(apperr.CodeVoiceMeetingClipRejected, apperr.WithOp("correction.meeting_clip.fact"))
	}
	if fact.EndSample-fact.StartSample > 60*16000 {
		return voiceservice.VoiceSample{}, apperr.Biz(apperr.CodeVoiceMeetingClipRejected, apperr.WithOp("correction.meeting_clip.duration"))
	}
	samples, err := service.audio.Read(ctx, fact.MeetingID, fact.StartSample, fact.EndSample)
	if err != nil || len(samples) == 0 {
		return voiceservice.VoiceSample{}, apperr.Biz(apperr.CodeVoiceMeetingClipRejected, apperr.WithOp("correction.meeting_clip.audio"))
	}
	result, err := service.enrollment.PrepareMeetingPCM(ctx, command.RequestID, fact.MemberID, fact.MeetingID, fact.UtteranceID, command.EnvironmentKind, samples)
	if err != nil {
		return voiceservice.VoiceSample{}, fmt.Errorf("会议片段声纹录入失败：%w", err)
	}
	return result, nil
}
