//go:build !windows

package main

import (
	"context"
	"os/exec"
)

func ensureSingleInstance(appName string) {
	// Linux / Unix では多重起動ロック不要
}

func buildClientShellCommand(ctx context.Context, cmdStr string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", cmdStr)
}

func decodeClientConsoleOutput(raw []byte) string {
	return string(raw)
}
