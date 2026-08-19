//go:build windows

package svc

import "log/slog"

// newPlatformRunner Windows 开发环境:本地无 runit/termux-services,
// 使用模拟执行器,便于开发验证 API 与 UI(生产 Termux 走真实执行器)。
func newPlatformRunner(log *slog.Logger) Runner {
	return NewMockRunner()
}
