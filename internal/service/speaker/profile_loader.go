package speaker

import (
	"errors"
	"fmt"
	"os"

	domain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
)

const maxMatchingProfileBytes = 64 * 1024

// LoadMatchingProfile 从固定路径读取严格档案，并将缺失与不匹配映射为稳定业务错误。
func LoadMatchingProfile(path string, expected domain.ModelIdentity) (domain.MatchingProfile, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.MatchingProfile{}, apperr.Biz(apperr.CodeSpeakerProfileMissing, apperr.WithOp("speaker.profile.stat"))
		}
		return domain.MatchingProfile{}, apperr.Dependency(apperr.CodeSpeakerProfileMismatch, err, apperr.WithOp("speaker.profile.stat"))
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxMatchingProfileBytes {
		return domain.MatchingProfile{}, apperr.Biz(apperr.CodeSpeakerProfileMismatch, apperr.WithOp("speaker.profile.size"))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return domain.MatchingProfile{}, apperr.Dependency(apperr.CodeSpeakerProfileMismatch, err, apperr.WithOp("speaker.profile.read"))
	}
	profile, err := domain.ParseMatchingProfile(content, expected)
	if err != nil {
		return domain.MatchingProfile{}, apperr.Dependency(
			apperr.CodeSpeakerProfileMismatch,
			fmt.Errorf("校准档案校验失败: %w", err),
			apperr.WithOp("speaker.profile.parse"),
		)
	}
	return profile, nil
}
