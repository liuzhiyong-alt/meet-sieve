//go:build !darwin && !windows

package network

import "context"

// resolveDefaultRoute 在非目标平台上明确返回未解析，不猜测默认网卡。
func resolveDefaultRoute(_ context.Context) (string, string, bool) {
	return "", "", false
}
