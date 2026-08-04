//go:build windows

package systemopen

import (
	"context"
	"os/exec"
)

func openFile(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", path).Run()
}
func revealFile(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "explorer.exe", "/select,", path).Run()
}
func openURL(ctx context.Context, target string) error {
	return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target).Run()
}
