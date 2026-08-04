//go:build darwin

package systemopen

import (
	"context"
	"os/exec"
)

func openFile(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "open", path).Run()
}
func revealFile(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "open", "-R", path).Run()
}
func openURL(ctx context.Context, target string) error {
	return exec.CommandContext(ctx, "open", target).Run()
}
