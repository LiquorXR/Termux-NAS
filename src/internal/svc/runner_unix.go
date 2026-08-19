//go:build !windows

package svc

import "log/slog"

// newPlatformRunner 非 Windows 平台使用真实命令执行器。
func newPlatformRunner(log *slog.Logger) Runner {
	return ExecRunner{}
}
