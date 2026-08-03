package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	agentrepository "meet-sieve/internal/repository/agent"
)

const recoveryPrompt = "请读取当前会议目录中的原始记录和资源索引，恢复会议上下文。"

var stableThreadID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// RecoveryCommands 是详情页可复制的两种恢复方式。
type RecoveryCommands struct {
	ThreadCommand    string
	DirectoryCommand string
}

// RecoveryCommandService 只从本地可信事实投影命令，不执行任何 shell。
type RecoveryCommandService struct {
	repository *agentrepository.Repository
	workspace  string
	platform   string
}

// NewRecoveryCommandService 创建当前平台的恢复命令投影服务。
func NewRecoveryCommandService(repository *agentrepository.Repository, workspace string) *RecoveryCommandService {
	return &RecoveryCommandService{repository: repository, workspace: workspace, platform: runtime.GOOS}
}

// Get 返回会议最近 thread 和可信会议目录对应的命令文本。
func (service *RecoveryCommandService) Get(ctx context.Context, meetingID string) (RecoveryCommands, error) {
	if service == nil || service.repository == nil || service.workspace == "" || meetingID == "" {
		return RecoveryCommands{}, fmt.Errorf("恢复命令服务未初始化")
	}
	meeting, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return RecoveryCommands{}, err
	}
	session, err := service.repository.GetLatestSession(ctx, meetingID)
	if err != nil {
		return RecoveryCommands{}, err
	}
	if session.ThreadID == nil {
		return RecoveryCommands{}, fmt.Errorf("会议没有可恢复的 Codex thread")
	}
	directory, err := trustedMeetingDirectory(service.workspace, meeting.RelativeDir)
	if err != nil {
		return RecoveryCommands{}, err
	}
	return BuildRecoveryCommands(service.platform, *session.ThreadID, directory)
}

// BuildRecoveryCommands 按目标平台构造可复制文本，严格拒绝不稳定 thread ID。
func BuildRecoveryCommands(platform string, threadID string, meetingDirectory string) (RecoveryCommands, error) {
	if !stableThreadID.MatchString(threadID) || !isAbsoluteForPlatform(platform, meetingDirectory) {
		return RecoveryCommands{}, fmt.Errorf("恢复命令参数无效")
	}
	quote := quotePOSIX
	if platform == "windows" {
		quote = quotePowerShell
	}
	return RecoveryCommands{
		ThreadCommand:    "codex resume " + threadID,
		DirectoryCommand: "codex -C " + quote(meetingDirectory) + " " + quote(recoveryPrompt),
	}, nil
}

// isAbsoluteForPlatform 避免用宿主平台规则误判待生成的 Windows 命令。
func isAbsoluteForPlatform(platform string, value string) bool {
	if strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	if platform == "windows" {
		return regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(value) || strings.HasPrefix(value, `\\`)
	}
	return filepath.IsAbs(value)
}

// quotePOSIX 使用单引号并安全拆分正文中的单引号。
func quotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// quotePowerShell 使用单引号，并按 PowerShell 规则把单引号重复一次。
func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
