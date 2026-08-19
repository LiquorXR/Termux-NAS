package svc

import (
	"context"
	"fmt"
	"strings"
)

// MockRunner 模拟执行器(跨平台可用,供测试与 Windows 开发调试):
// 本地无 runit/termux-services 时用内存状态模拟 sv 命令,
// 便于开发验证 API 与 UI(生产 Termux 走真实执行器)。
type MockRunner struct {
	// 服务名 → 是否运行(默认全部停止)
	state map[string]bool
}

// NewMockRunner 创建模拟执行器。
func NewMockRunner() *MockRunner {
	return &MockRunner{state: make(map[string]bool)}
}

// SetRunning 设置服务运行状态(测试辅助)。
func (m *MockRunner) SetRunning(name string, running bool) {
	m.state[name] = running
}

// Running 查询服务运行状态(测试辅助)。
func (m *MockRunner) Running(name string) bool {
	return m.state[name]
}

// Run 实现 Runner 接口。
func (m *MockRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	switch name {
	case "sv":
		return m.runSV(args...)
	case "sv-enable", "sv-disable":
		if len(args) < 1 {
			return "", fmt.Errorf("缺少服务名")
		}
		m.state[args[0]] = name == "sv-enable"
		return "", nil
	case "ls":
		// 模拟 ls 检查 down 文件
		if len(args) < 1 {
			return "", fmt.Errorf("缺少参数")
		}
		if strings.Contains(args[len(args)-1], "/down") {
			return "", fmt.Errorf("down 文件不存在")
		}
		return "", nil
	default:
		return "", fmt.Errorf("命令不可用: %s", name)
	}
}

func (m *MockRunner) runSV(args ...string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("sv 用法: sv <status|start|stop|restart> <name>")
	}
	op, svcName := args[0], args[1]
	switch op {
	case "start":
		m.state[svcName] = true
		return "", nil
	case "stop":
		m.state[svcName] = false
		return "", nil
	case "restart":
		m.state[svcName] = true
		return "", nil
	case "status":
		if m.state[svcName] {
			return fmt.Sprintf("run: %s: (pid 12345) 10s", svcName), nil
		}
		return fmt.Sprintf("down: %s: 5s, normally up", svcName), nil
	default:
		return "", fmt.Errorf("未知 sv 操作: %s", op)
	}
}
