// Package svc 实现服务控制模块(M5):基于 termux-services(runit)的服务启停/状态/自启。
//
// 平台差异:
//   - Termux/Linux:直接调用 sv / sv-enable / sv-disable 命令
//   - Windows(开发调试):降级为模拟实现,便于本地验证 API 与 UI
//
// 开发文档 §5.2 服务控制:进程管理封装(Svc() API),基于 termux-services。
package svc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// Status 服务运行状态。
type Status string

const (
	StatusRunning Status = "running"
	StatusDown    Status = "down"
	StatusUnknown Status = "unknown"
	StatusError   Status = "error"
)

// Service 一个受管服务条目。
type Service struct {
	Name        string `json:"name"`         // 服务名(对应 $PREFIX/var/service/<name>)
	DisplayName string `json:"display_name"` // 展示名
	Description string `json:"description"`  // 说明
}

// Info 服务状态信息(API 输出)。
type Info struct {
	Service
	State      Status `json:"state"`
	Autostart  bool   `json:"autostart"`   // 开机自启(已 enable)
	Pid        int    `json:"pid,omitempty"` // 运行中 PID(sv status 解析)
	Uptime     string `json:"uptime,omitempty"`
	Detail     string `json:"detail,omitempty"` // 原始输出摘要
	LastError  string `json:"last_err,omitempty"`
}

// 内置服务目录(可被用户配置覆盖)。
var DefaultServices = []Service{
	{Name: "sshd", DisplayName: "SSH", Description: "远程终端登录"},
	{Name: "samba", DisplayName: "Samba", Description: "Windows 文件共享(SMB)"},
	{Name: "nginx", DisplayName: "Nginx", Description: "Web 服务器"},
	{Name: "aria2", DisplayName: "aria2", Description: "下载服务"},
	{Name: "cron", DisplayName: "Cron", Description: "定时任务"},
	{Name: "mysql", DisplayName: "MySQL", Description: "数据库服务"},
}

// Runner 执行外部命令的接口(便于测试注入假执行器)。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner 基于 os/exec 的真实执行器。
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Controller 服务控制器(平台无关逻辑 + 注入 Runner)。
type Controller struct {
	services []Service
	runner   Runner
	log      *slog.Logger
}

// New 创建服务控制器,runner 为空时使用平台默认执行器。
func New(services []Service, runner Runner, log *slog.Logger) *Controller {
	if len(services) == 0 {
		services = DefaultServices
	}
	if runner == nil {
		runner = newPlatformRunner(log)
	}
	return &Controller{services: services, runner: runner, log: log}
}

// List 返回全部受管服务及状态。
func (c *Controller) List(ctx context.Context) []Info {
	out := make([]Info, 0, len(c.services))
	for _, s := range c.services {
		out = append(out, c.status(ctx, s))
	}
	return out
}

// Get 返回单个服务状态。
func (c *Controller) Get(ctx context.Context, name string) (Info, error) {
	for _, s := range c.services {
		if s.Name == name {
			return c.status(ctx, s), nil
		}
	}
	return Info{}, fmt.Errorf("未知服务: %s", name)
}

// Start 启动服务。
func (c *Controller) Start(ctx context.Context, name string) error {
	_, err := c.runner.Run(ctx, "sv", "start", name)
	if err != nil {
		return fmt.Errorf("启动 %s 失败: %w", name, err)
	}
	c.log.Info("服务已启动", "svc", name)
	return nil
}

// Stop 停止服务。
func (c *Controller) Stop(ctx context.Context, name string) error {
	_, err := c.runner.Run(ctx, "sv", "stop", name)
	if err != nil {
		return fmt.Errorf("停止 %s 失败: %w", name, err)
	}
	c.log.Info("服务已停止", "svc", name)
	return nil
}

// Restart 重启服务。
func (c *Controller) Restart(ctx context.Context, name string) error {
	_, err := c.runner.Run(ctx, "sv", "restart", name)
	if err != nil {
		return fmt.Errorf("重启 %s 失败: %w", name, err)
	}
	c.log.Info("服务已重启", "svc", name)
	return nil
}

// SetAutostart 设置开机自启(sv-enable / sv-disable)。
func (c *Controller) SetAutostart(ctx context.Context, name string, enabled bool) error {
	cmd := "sv-disable"
	if enabled {
		cmd = "sv-enable"
	}
	_, err := c.runner.Run(ctx, cmd, name)
	if err != nil {
		// 部分环境 sv-enable 不在 PATH;兜底尝试 sv enable
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "Exec format") {
			args := []string{"enable", name}
			if !enabled {
				args[0] = "disable"
			}
			if _, err2 := c.runner.Run(ctx, "sv", args...); err2 == nil {
				c.log.Info("服务自启已设置", "svc", name, "enabled", enabled)
				return nil
			}
		}
		return fmt.Errorf("设置 %s 自启失败: %w", name, err)
	}
	c.log.Info("服务自启已设置", "svc", name, "enabled", enabled)
	return nil
}

// status 查询单个服务状态并解析。
func (c *Controller) status(ctx context.Context, s Service) Info {
	info := Info{Service: s, State: StatusUnknown}
	out, err := c.runner.Run(ctx, "sv", "status", s.Name)
	if err != nil {
		if isCmdNotFound(err) {
			info.State = StatusError
			info.LastError = "termux-services 未安装(sv 命令不可用)"
			return info
		}
		info.State = StatusError
		info.LastError = err.Error()
		info.Detail = out
		return info
	}
	info.Detail = strings.TrimSpace(out)
	parseSvStatus(out, &info)
	// 自启状态:sv-enable 未提供查询,通过 ls $PREFIX/var/service/<name>/down 间接判断
	autostart, _ := c.checkAutostart(ctx, s.Name)
	info.Autostart = autostart
	return info
}

// checkAutostart 通过 service 目录中的 down 文件判断自启。
// runit 约定:存在 down 文件 = 不自动启动;不存在 = 开机自启。
func (c *Controller) checkAutostart(ctx context.Context, name string) (bool, error) {
	// sv 命令本身不带 enable 查询;通过文件系统探测
	out, err := c.runner.Run(ctx, "ls", "-d", "/data/data/com.termux/files/usr/var/service/"+name+"/down")
	if err == nil && strings.Contains(out, "down") {
		return false, nil
	}
	return true, nil // 目录存在且无 down 文件 = 自启
}

// parseSvStatus 解析 `sv status <name>` 输出。
// runit 输出示例:
//
//	run: sshd: (pid 123) 45s
//	down: sshd: 60s, normally up
//	down: nginx: 5s, normally down
func parseSvStatus(out string, info *Info) {
	out = strings.TrimSpace(out)
	switch {
	case strings.HasPrefix(out, "run:"):
		info.State = StatusRunning
		if _, rest, ok := strings.Cut(out, "(pid "); ok {
			if pidStr, _, ok2 := strings.Cut(rest, ")"); ok2 {
				fmt.Sscanf(pidStr, "%d", &info.Pid)
			}
		}
		if _, rest, ok := strings.Cut(out, ")"); ok {
			info.Uptime = strings.TrimSpace(strings.TrimPrefix(rest, ")"))
		}
	case strings.HasPrefix(out, "down:"):
		info.State = StatusDown
		if _, rest, ok := strings.Cut(out, ":"); ok {
			info.Uptime = strings.TrimSpace(strings.Split(rest, ",")[0])
		}
		if strings.Contains(out, "normally up") {
			info.Autostart = true
		}
	default:
		info.State = StatusUnknown
	}
}

// isCmdNotFound 判断错误是否为"命令不存在"。
func isCmdNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ee *exec.Error
	if errors.As(err, &ee) && errors.Is(ee.Err, exec.ErrNotFound) {
		return true
	}
	return strings.Contains(err.Error(), "executable file not found")
}
