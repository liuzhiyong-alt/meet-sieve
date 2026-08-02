package singleinstance

import "strconv"

const activationPipePrefix = `\\.\pipe\MeetSieve.App.Activate.v1.`

// WindowsMutexName 是应用、NSIS 安装器和卸载器共享的全局 mutex 名称。
const WindowsMutexName = `Global\MeetSieve.App.Instance.v1`

// ActivationPipeName 返回当前 Windows 登录会话的固定激活管道名称。
func ActivationPipeName(sessionID uint32) string {
	return activationPipePrefix + strconv.FormatUint(uint64(sessionID), 10)
}
