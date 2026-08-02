package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	voicerepository "meet-sieve/internal/repository/voice"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EncoderProvider 返回当前已校验的声纹编码器。
type EncoderProvider func() (port.VoiceEncoder, error)

// VoiceEnrollmentDependencies 描述声纹录入服务的显式依赖。
type VoiceEnrollmentDependencies struct {
	Members      *peoplerepository.MemberRepository
	Repository   *voicerepository.SampleRepository
	Files        *SampleFileStore
	Transactions *database.TransactionManager
	Encoder      EncoderProvider
	IDs          identity.Generator
	Clock        clock.Clock
}

// VoiceSample 是声纹管理页面使用的安全业务投影。
type VoiceSample struct {
	ID              string
	MemberID        string
	RelativePath    string
	DurationMS      int64
	SourceKind      string
	SourceName      string
	EnvironmentKind string
	ProcessingState string
	QualityState    string
	QualityCode     string
	CreatedAt       int64
}

// VoiceEnrollmentService 编排规范化、质量、文件、模型与 SQLite 短事务。
type VoiceEnrollmentService struct {
	members      *peoplerepository.MemberRepository
	repository   *voicerepository.SampleRepository
	files        *SampleFileStore
	transactions *database.TransactionManager
	encoder      EncoderProvider
	ids          identity.Generator
	clock        clock.Clock

	mu      sync.Mutex
	active  map[string]struct{}
	workers chan struct{}
}

// NewVoiceEnrollmentService 创建最多并行处理两个不同成员的录入服务。
func NewVoiceEnrollmentService(dependencies VoiceEnrollmentDependencies) *VoiceEnrollmentService {
	return &VoiceEnrollmentService{
		members: dependencies.Members, repository: dependencies.Repository, files: dependencies.Files,
		transactions: dependencies.Transactions, encoder: dependencies.Encoder,
		ids: dependencies.IDs, clock: dependencies.Clock,
		active: make(map[string]struct{}), workers: make(chan struct{}, 2),
	}
}

// PrepareImported 处理用户在会前选择的单人 PCM WAV 文件。
func (service *VoiceEnrollmentService) PrepareImported(ctx context.Context, memberID string, sourceName string, environmentKind string, wav []byte) (VoiceSample, error) {
	name, err := cleanSourceName(sourceName)
	if err != nil {
		return VoiceSample{}, err
	}
	return service.prepare(ctx, memberID, "imported", name, environmentKind, wav, meetingSampleSource{})
}

// PrepareRecorded 处理软件内短录音产生的 16kHz PCM16 单声道内容。
func (service *VoiceEnrollmentService) PrepareRecorded(ctx context.Context, memberID string, environmentKind string, recording RecordingResult) (VoiceSample, error) {
	if recording.SampleRate != canonicalSampleRate || len(recording.PCM) == 0 || len(recording.PCM)%2 != 0 {
		return VoiceSample{}, apperr.Biz(apperr.CodeVoiceWAVInvalid, apperr.WithOp("voice.enrollment.recorded_pcm"))
	}
	samples := make([]int16, len(recording.PCM)/2)
	for index := range samples {
		samples[index] = int16(binary.LittleEndian.Uint16(recording.PCM[index*2:]))
	}
	return service.prepare(ctx, memberID, "recorded", "", environmentKind, encodePCM16WAV(samples, canonicalSampleRate), meetingSampleSource{})
}

type meetingSampleSource struct {
	requestID   string
	meetingID   string
	utteranceID string
}

// PrepareMeetingPCM 复用同一规范化、质量、文件和 embedding 事务处理已确认会议原片段。
func (service *VoiceEnrollmentService) PrepareMeetingPCM(ctx context.Context, requestID string, memberID string, meetingID string, utteranceID string, environmentKind string, samples []int16) (VoiceSample, error) {
	for _, value := range []string{requestID, memberID, meetingID, utteranceID} {
		if uuid.Validate(value) != nil {
			return VoiceSample{}, apperr.Biz(apperr.CodeVoiceMeetingClipRejected, apperr.WithOp("voice.enrollment.meeting_clip.validate"))
		}
	}
	existing, found, err := service.repository.GetByRequest(ctx, requestID)
	if err != nil {
		return VoiceSample{}, err
	}
	if found {
		if existing.MemberID != memberID || existing.SourceMeetingID == nil || *existing.SourceMeetingID != meetingID || existing.SourceUtteranceID == nil || *existing.SourceUtteranceID != utteranceID {
			return VoiceSample{}, apperr.Biz(apperr.CodeCorrectionIdempotencyConflict, apperr.WithOp("voice.enrollment.meeting_clip.idempotency"))
		}
		return mapVoiceSample(existing), nil
	}
	if len(samples) == 0 {
		return VoiceSample{}, apperr.Biz(apperr.CodeVoiceMeetingClipRejected, apperr.WithOp("voice.enrollment.meeting_clip.empty"))
	}
	source := meetingSampleSource{requestID: requestID, meetingID: meetingID, utteranceID: utteranceID}
	return service.prepare(ctx, memberID, "meeting_clip", "", environmentKind, encodePCM16WAV(samples, canonicalSampleRate), source)
}

// ListSamples 返回成员声纹样本，不读取音频或 embedding 正文。
func (service *VoiceEnrollmentService) ListSamples(ctx context.Context, memberID string) ([]VoiceSample, error) {
	if err := service.requireActiveMember(ctx, memberID); err != nil {
		return nil, err
	}
	samples, err := service.repository.ListByMember(ctx, memberID)
	if err != nil {
		return nil, err
	}
	result := make([]VoiceSample, 0, len(samples))
	for _, sample := range samples {
		result = append(result, mapVoiceSample(sample))
	}
	return result, nil
}

// DeleteSample 永久删除属于指定成员的单个声纹样本及其 embedding。
func (service *VoiceEnrollmentService) DeleteSample(ctx context.Context, memberID string, sampleID string) error {
	if err := service.requireActiveMember(ctx, memberID); err != nil {
		return err
	}
	sample, found, err := service.repository.GetByID(ctx, sampleID)
	if err != nil {
		return err
	}
	if !found || sample.MemberID != memberID {
		return apperr.Biz(apperr.CodeNotFound, apperr.WithOp("voice.sample.delete"))
	}
	return service.files.DeleteSample(ctx, sample)
}

// DeleteAllSamples 逐项执行可恢复删除；首个失败会保留剩余样本供用户重试。
func (service *VoiceEnrollmentService) DeleteAllSamples(ctx context.Context, memberID string) error {
	if err := service.requireActiveMember(ctx, memberID); err != nil {
		return err
	}
	samples, err := service.repository.ListByMember(ctx, memberID)
	if err != nil {
		return err
	}
	for _, sample := range samples {
		if err := service.files.DeleteSample(ctx, sample); err != nil {
			return err
		}
	}
	return nil
}

// ResumeProcessing 继续处理启动恢复后的 pending 样本；单项失败不会阻断后续样本。
func (service *VoiceEnrollmentService) ResumeProcessing(ctx context.Context) error {
	if err := service.validateDependencies(); err != nil {
		return err
	}
	samples, err := service.repository.ListProcessing(ctx)
	if err != nil {
		return err
	}
	failed := 0
	for _, sample := range samples {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := service.resumeSample(ctx, sample); err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d 个待处理声纹样本恢复失败", failed)
	}
	return nil
}

// prepare 执行两种来源共用的声纹录入主流程。
func (service *VoiceEnrollmentService) prepare(ctx context.Context, memberID string, sourceKind string, sourceName string, environmentKind string, wav []byte, source meetingSampleSource) (VoiceSample, error) {
	if err := service.validateDependencies(); err != nil {
		return VoiceSample{}, err
	}
	if err := ValidateEnvironmentKind(environmentKind); err != nil {
		return VoiceSample{}, err
	}
	release, err := service.begin(ctx, memberID)
	if err != nil {
		return VoiceSample{}, err
	}
	defer release()
	if err := service.requireActiveMember(ctx, memberID); err != nil {
		return VoiceSample{}, err
	}
	normalized, err := NormalizeWAV(ctx, wav)
	if err != nil {
		return VoiceSample{}, err
	}
	assessment, err := AnalyzeQuality(normalized.Samples, normalized.SampleRate, ProductionQualityThresholds())
	if err != nil {
		return VoiceSample{}, err
	}
	sample, err := service.createPendingSample(memberID, sourceKind, sourceName, environmentKind, normalized, source)
	if err != nil {
		return VoiceSample{}, err
	}
	if err := service.files.PersistPending(ctx, sample, normalized.WAV); err != nil {
		return VoiceSample{}, err
	}
	metricsJSON, err := json.Marshal(assessment.Metrics)
	if err != nil {
		return VoiceSample{}, fmt.Errorf("序列化声纹质量指标失败: %w", err)
	}
	if !assessment.Passed {
		return service.completeRejected(ctx, sample, assessment.Code, string(metricsJSON))
	}
	return service.completeAccepted(ctx, sample, normalized, string(metricsJSON))
}

// completeAccepted 在模型推理完成后以一个短事务提交 embedding 和 ready 状态。
func (service *VoiceEnrollmentService) completeAccepted(ctx context.Context, sample models.VoiceSample, normalized NormalizedWAV, metricsJSON string) (VoiceSample, error) {
	encoder, err := service.encoder()
	if err != nil || encoder == nil {
		return VoiceSample{}, apperr.Dependency(apperr.CodeVoiceModelUnavailable, err, apperr.WithOp("voice.enrollment.encoder"))
	}
	embedding, err := encoder.Encode(ctx, toAudioPCM(normalized.Samples))
	if err != nil {
		return VoiceSample{}, apperr.Dependency(apperr.CodeVoiceEmbeddingFailed, err, apperr.WithOp("voice.enrollment.encode"))
	}
	modelInfo := encoder.ModelInfo()
	blob, err := EncodeEmbeddingBlob(embedding, modelInfo.Dimension)
	if err != nil {
		return VoiceSample{}, apperr.Dependency(apperr.CodeVoiceEmbeddingFailed, err, apperr.WithOp("voice.enrollment.embedding"))
	}
	now := service.clock.Now().UnixMilli()
	embeddingID := service.ids.New()
	if uuid.Validate(embeddingID) != nil {
		return VoiceSample{}, fmt.Errorf("生成声纹 embedding UUID 失败")
	}
	record := models.VoiceEmbedding{
		ID: embeddingID, VoiceSampleID: sample.ID, ModelID: modelInfo.ID, ModelVersion: modelInfo.Version,
		ModelSHA256: modelInfo.SHA256, Dimension: modelInfo.Dimension, Embedding: blob, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.CompleteAccepted(ctx, tx, sample.ID, metricsJSON, record)
	}); err != nil {
		return VoiceSample{}, err
	}
	sample.ProcessingState, sample.QualityState, sample.QualityMetricsJSON = "ready", "accepted", &metricsJSON
	sample.UpdatedAt = now
	return mapVoiceSample(sample), nil
}

// resumeSample 从已校验正式 WAV 重算质量，并继续原本的 accepted/rejected 分支。
func (service *VoiceEnrollmentService) resumeSample(ctx context.Context, sample models.VoiceSample) error {
	wav, err := service.files.ReadVerifiedWAV(sample)
	if err != nil {
		return err
	}
	normalized, err := NormalizeWAV(ctx, wav)
	if err != nil {
		return err
	}
	assessment, err := AnalyzeQuality(normalized.Samples, normalized.SampleRate, ProductionQualityThresholds())
	if err != nil {
		return err
	}
	metricsJSON, err := json.Marshal(assessment.Metrics)
	if err != nil {
		return err
	}
	if !assessment.Passed {
		_, err = service.completeRejected(ctx, sample, assessment.Code, string(metricsJSON))
		return err
	}
	_, err = service.completeAccepted(ctx, sample, normalized, string(metricsJSON))
	return err
}

// completeRejected 保存可解释质量结论且不创建 embedding。
func (service *VoiceEnrollmentService) completeRejected(ctx context.Context, sample models.VoiceSample, code QualityCode, metricsJSON string) (VoiceSample, error) {
	now := service.clock.Now().UnixMilli()
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.CompleteRejected(ctx, tx, sample.ID, string(code), metricsJSON, now)
	}); err != nil {
		return VoiceSample{}, err
	}
	qualityCode := string(code)
	sample.ProcessingState, sample.QualityState = "ready", "rejected"
	sample.QualityCode, sample.QualityMetricsJSON, sample.UpdatedAt = &qualityCode, &metricsJSON, now
	return mapVoiceSample(sample), nil
}

// createPendingSample 生成只使用 UUID 的正式相对路径和数据库记录。
func (service *VoiceEnrollmentService) createPendingSample(memberID string, sourceKind string, sourceName string, environmentKind string, normalized NormalizedWAV, source meetingSampleSource) (models.VoiceSample, error) {
	sampleID := service.ids.New()
	if uuid.Validate(sampleID) != nil {
		return models.VoiceSample{}, fmt.Errorf("生成声纹样本 UUID 失败")
	}
	now := service.clock.Now().UnixMilli()
	var storedSourceName *string
	if sourceName != "" {
		storedSourceName = &sourceName
	}
	requestID, meetingID, utteranceID := optionalSourceValue(source.requestID), optionalSourceValue(source.meetingID), optionalSourceValue(source.utteranceID)
	return models.VoiceSample{
		ID: sampleID, MemberID: memberID,
		RelativePath: filepath.ToSlash(filepath.Join("data", "voice-samples", memberID, sampleID+".wav")),
		DurationMS:   normalized.DurationMS, SampleRate: 16000, Channels: 1, BitDepth: 16,
		SizeBytes: int64(len(normalized.WAV)), SHA256: normalized.SHA256,
		SourceKind: sourceKind, SourceName: storedSourceName, RequestID: requestID,
		SourceMeetingID: meetingID, SourceUtteranceID: utteranceID, EnvironmentKind: environmentKind,
		ProcessingState: "processing", QualityState: "pending", CreatedAt: now, UpdatedAt: now,
	}, nil
}

// optionalSourceValue 为非会议来源保留数据库 NULL 语义。
func optionalSourceValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// begin 限制同一成员并发录入，并把全局 CPU 密集任务固定为最多两个。
func (service *VoiceEnrollmentService) begin(ctx context.Context, memberID string) (func(), error) {
	service.mu.Lock()
	if _, exists := service.active[memberID]; exists {
		service.mu.Unlock()
		return nil, apperr.Biz(apperr.CodeConflict, apperr.WithOp("voice.enrollment.concurrent"))
	}
	service.active[memberID] = struct{}{}
	service.mu.Unlock()
	select {
	case service.workers <- struct{}{}:
		return func() {
			<-service.workers
			service.mu.Lock()
			delete(service.active, memberID)
			service.mu.Unlock()
		}, nil
	case <-ctx.Done():
		service.mu.Lock()
		delete(service.active, memberID)
		service.mu.Unlock()
		return nil, ctx.Err()
	}
}

// requireActiveMember 保证样本只关联当前活动成员。
func (service *VoiceEnrollmentService) requireActiveMember(ctx context.Context, memberID string) error {
	if uuid.Validate(memberID) != nil {
		return apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("voice.enrollment.member_id"))
	}
	_, found, err := service.members.GetActiveByID(ctx, memberID)
	if err != nil {
		return err
	}
	if !found {
		return apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("voice.enrollment.member"))
	}
	return nil
}

// validateDependencies 在产生文件或数据库副作用前检查依赖。
func (service *VoiceEnrollmentService) validateDependencies() error {
	if service == nil || service.members == nil || service.repository == nil || service.files == nil || service.transactions == nil || service.encoder == nil || service.ids == nil || service.clock == nil {
		return fmt.Errorf("声纹录入服务依赖未初始化")
	}
	return nil
}

// markEmbeddingFailed 把无法编码的样本排除出 readiness 与重建查询。
func (service *VoiceEnrollmentService) markEmbeddingFailed(ctx context.Context, sampleID string) error {
	return service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.MarkFailed(ctx, tx, sampleID, apperr.CodeVoiceEmbeddingFailed.ErrorCode)
	})
}

// toAudioPCM 将规范化 int16 样本转换为 torchaudio 一致的 [-1,1) 标度。
func toAudioPCM(samples []int16) port.AudioPCM {
	result := make([]float32, len(samples))
	for index, sample := range samples {
		result[index] = float32(sample) / 32768
	}
	return port.AudioPCM{Samples: result, SampleRate: canonicalSampleRate}
}

// cleanSourceName 只保留跨平台 basename，并拒绝控制字符。
func cleanSourceName(value string) (string, error) {
	name := filepath.Base(strings.ReplaceAll(strings.TrimSpace(value), `\`, "/"))
	if name == "" || name == "." {
		return "", apperr.Biz(apperr.CodeVoiceWAVInvalid, apperr.WithOp("voice.enrollment.source_name"))
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", apperr.Biz(apperr.CodeVoiceWAVInvalid, apperr.WithOp("voice.enrollment.source_name"))
		}
	}
	return name, nil
}

// ValidateEnvironmentKind 校验第一期固定的录音环境枚举。
func ValidateEnvironmentKind(value string) error {
	switch value {
	case "quiet", "meeting_room", "other":
		return nil
	default:
		return apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("voice.enrollment.environment"))
	}
}

// mapVoiceSample 把数据库模型映射为不包含指标 JSON 和文件哈希的业务投影。
func mapVoiceSample(sample models.VoiceSample) VoiceSample {
	result := VoiceSample{
		ID: sample.ID, MemberID: sample.MemberID, RelativePath: sample.RelativePath,
		DurationMS: sample.DurationMS, SourceKind: sample.SourceKind, EnvironmentKind: sample.EnvironmentKind,
		ProcessingState: sample.ProcessingState, QualityState: sample.QualityState, CreatedAt: sample.CreatedAt,
	}
	if sample.SourceName != nil {
		result.SourceName = *sample.SourceName
	}
	if sample.QualityCode != nil {
		result.QualityCode = *sample.QualityCode
	}
	return result
}
