package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	appbootstrap "meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	voicerepository "meet-sieve/internal/repository/voice"
	voiceservice "meet-sieve/internal/service/voice"
)

// VoiceWorkspaceServices 为当前工作目录持有唯一声纹业务服务与恢复状态。
type VoiceWorkspaceServices struct {
	coordinator *appbootstrap.Coordinator
	module      *VoiceModule

	mu         sync.Mutex
	activePath string
	enrollment *voiceservice.VoiceEnrollmentService
	rebuild    *voiceservice.RebuildRunner
}

// NewVoiceWorkspaceServices 创建按当前 ready 工作目录延迟装配的声纹服务提供器。
func NewVoiceWorkspaceServices(coordinator *appbootstrap.Coordinator, module *VoiceModule) *VoiceWorkspaceServices {
	return &VoiceWorkspaceServices{coordinator: coordinator, module: module}
}

// Current 返回当前工作目录对应的共享服务，首次构造时执行可恢复文件检查。
func (services *VoiceWorkspaceServices) Current() (*voiceservice.VoiceEnrollmentService, *voiceservice.RebuildRunner, error) {
	if services == nil || services.coordinator == nil {
		return nil, nil, fmt.Errorf("声纹工作目录服务不可用")
	}
	settings := services.coordinator.GetWorkspaceSettings()
	if settings.ActivePath == "" {
		return nil, nil, fmt.Errorf("工作目录尚未就绪")
	}
	services.mu.Lock()
	defer services.mu.Unlock()
	if services.activePath == settings.ActivePath && services.enrollment != nil && services.rebuild != nil {
		return services.enrollment, services.rebuild, nil
	}
	reader, transactions, err := services.coordinator.BusinessDatabase()
	if err != nil {
		return nil, nil, err
	}
	repository := voicerepository.NewSampleRepository(reader)
	files := voiceservice.NewSampleFileStore(filepath.Clean(settings.ActivePath), repository, transactions)
	if err := files.RecoverTrash(context.Background()); err != nil {
		return nil, nil, err
	}
	if err := files.RecoverPending(context.Background()); err != nil {
		return nil, nil, err
	}
	if err := files.CleanupOrphanedStaging(context.Background(), time.Now().Add(-24*time.Hour)); err != nil {
		return nil, nil, err
	}
	currentClock := clock.NewSystem()
	encoder := services.encoder
	enrollment := voiceservice.NewVoiceEnrollmentService(voiceservice.VoiceEnrollmentDependencies{
		Members: peoplerepository.NewMemberRepository(reader), Repository: repository, Files: files,
		Transactions: transactions, Encoder: encoder, IDs: identity.NewUUIDGenerator(), Clock: currentClock,
	})
	rebuild := voiceservice.NewRebuildRunner(voiceservice.RebuildDependencies{
		Repository: repository, Files: files, Transactions: transactions, Encoder: encoder,
		IDs: identity.NewUUIDGenerator(), Clock: currentClock,
	})
	// 模型未安装时保留 processing 状态；模型就绪后的首次访问会继续处理。
	if _, encoderErr := encoder(); encoderErr == nil {
		_ = enrollment.ResumeProcessing(context.Background())
	}
	services.activePath, services.enrollment, services.rebuild = settings.ActivePath, enrollment, rebuild
	return enrollment, rebuild, nil
}

// RecoverAndRebuild 在模型安装后继续 pending 样本并重建历史向量。
func (services *VoiceWorkspaceServices) RecoverAndRebuild(ctx context.Context) error {
	enrollment, rebuild, err := services.Current()
	if err != nil {
		return err
	}
	if err := enrollment.ResumeProcessing(ctx); err != nil {
		return err
	}
	_, err = rebuild.Run(ctx)
	return err
}

// encoder 延迟取得当前模型，模型缺失不会阻断工作目录和成员功能。
func (services *VoiceWorkspaceServices) encoder() (port.VoiceEncoder, error) {
	if services.module == nil {
		return nil, fmt.Errorf("声纹模型模块不可用")
	}
	return services.module.Encoder()
}
