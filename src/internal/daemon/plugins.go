package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// PluginState 插件生命周期状态机:
//
//	stopped → starting → running → stopping → stopped
//	                    ↘ crashed(非主动退出)→ stopped/restart
//	连续崩溃超过阈值 → crash-loop(停止自动重启,等待人工介入)
type PluginState string

const (
	StateStopped   PluginState = "stopped"
	StateStarting  PluginState = "starting"
	StateRunning   PluginState = "running"
	StateStopping  PluginState = "stopping"
	StateCrashed   PluginState = "crashed"
	StateCrashLoop PluginState = "crash-loop"
)

// 插件管理错误。
var (
	ErrPluginNotFound = errors.New("插件不存在")
	ErrAlreadyRunning = errors.New("插件已在运行")
	ErrNotRunning     = errors.New("插件未在运行")
	ErrCrashLoop      = errors.New("插件处于崩溃循环,已停止自动重启")
	ErrStartFailed    = errors.New("插件启动失败")
)

// PluginRegistration 插件进程启动后向 stdout 输出的注册 JSON(注册协议,M4)。
// 由 nasd 解析并登记;P1 仅定义结构,P2 接入 stdout 解析。
type PluginRegistration struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
	Nav     string `json:"nav"`
	Icon    string `json:"icon"`
}

// PluginInfo 插件磁盘元信息 + 运行状态(对外 API 输出)。
type PluginInfo struct {
	ID       string             `json:"id"`
	Path     string             `json:"path"`
	Size     int64              `json:"size"`
	ModTime  time.Time          `json:"mod_time"`
	State    PluginState        `json:"state"`
	PID      int                `json:"pid,omitempty"`      // 运行中进程 PID
	Restarts int                `json:"restarts"`           // 连续崩溃计数
	Reg      *PluginRegistration `json:"reg,omitempty"`      // 注册元信息(P2 起)
	LastErr  string             `json:"last_err,omitempty"` // 最近一次错误
}

// maxRestarts 连续崩溃最大次数,超过进入 crash-loop。
const maxRestarts = 3

// stopWaitTimeout 主动停止等待进程退出的超时。
const stopWaitTimeout = 5 * time.Second

// ManagedPlugin 一个受管插件实例。
type ManagedPlugin struct {
	mu     sync.Mutex
	info   PluginInfo
	cmd    *exec.Cmd          // 运行中的进程(running/starting 状态非空)
	cancel context.CancelFunc // 取消 → CommandContext 杀进程
	done   chan struct{}      // 进程退出后关闭(watchExit 回调)
}

// isExecutable 跨平台判断可执行文件。
// Unix:检查执行位;Windows:os.Stat 不提供执行位,按可执行扩展名判定。
func isExecutable(path string, mode os.FileMode) bool {
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".bat", ".cmd", ".com":
			return true
		}
		return false
	}
	return mode&0o111 != 0
}

// Manager 插件管理器(nasd 全权控制插件)。
type Manager struct {
	mu      sync.Mutex
	plugins map[string]*ManagedPlugin
	dir     string // ~/nas/plugins
	log     *slog.Logger
}

// NewManager 创建插件管理器。
func NewManager(pluginsDir string, log *slog.Logger) *Manager {
	return &Manager{
		plugins: make(map[string]*ManagedPlugin),
		dir:     pluginsDir,
		log:     log,
	}
}

// Scan 扫描 plugins 目录,登记新增插件、移除已删除插件(不启动进程)。
// 返回全部插件列表。已登记插件保留运行状态不被重置。
func (m *Manager) Scan() ([]PluginInfo, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if isExecutable(e.Name(), info.Mode()) {
			id := e.Name()
			seen[id] = true
			m.mu.Lock()
			mp, ok := m.plugins[id]
			if !ok {
				mp = &ManagedPlugin{}
				m.plugins[id] = mp
				m.log.Info("插件登记", "id", id)
			}
			m.mu.Unlock()

			mp.mu.Lock()
			old := mp.info
			if old.State == "" {
				old.State = StateStopped // 新登记插件的默认状态
			}
			mp.info = PluginInfo{
				ID:       id,
				Path:     filepath.Join(m.dir, id),
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				State:    old.State, // 保留既有运行状态
				PID:      old.PID,
				Restarts: old.Restarts,
				Reg:      old.Reg,
				LastErr:  old.LastErr,
			}
			mp.mu.Unlock()
		}
	}

	// 移除磁盘上已不存在的插件(仅限未运行状态;运行中不删避免悬空)
	m.mu.Lock()
	for id, mp := range m.plugins {
		if seen[id] {
			continue
		}
		mp.mu.Lock()
		running := mp.info.State == StateRunning || mp.info.State == StateStarting || mp.info.State == StateStopping
		mp.mu.Unlock()
		if !running {
			delete(m.plugins, id)
			m.log.Info("插件注销", "id", id)
		}
	}
	m.mu.Unlock()
	return m.List(), nil
}

// List 返回全部插件快照(供 API 输出)。
func (m *Manager) List() []PluginInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PluginInfo, 0, len(m.plugins))
	for _, mp := range m.plugins {
		mp.mu.Lock()
		info := mp.info
		if mp.cmd != nil && mp.cmd.Process != nil {
			info.PID = mp.cmd.Process.Pid
		}
		mp.mu.Unlock()
		out = append(out, info)
	}
	return out
}

// Get 返回单个插件快照。
func (m *Manager) Get(id string) (PluginInfo, error) {
	mp, err := m.get(id)
	if err != nil {
		return PluginInfo{}, err
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()
	info := mp.info
	if mp.cmd != nil && mp.cmd.Process != nil {
		info.PID = mp.cmd.Process.Pid
	}
	return info, nil
}

func (m *Manager) get(id string) (*ManagedPlugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mp, ok := m.plugins[id]
	if !ok {
		return nil, ErrPluginNotFound
	}
	return mp, nil
}

// Start 启动插件进程(P1:直接拉起,等待注册协议接入)。
// 已在运行返回 ErrAlreadyRunning;崩溃循环状态拒绝启动。
func (m *Manager) Start(id string) error {
	mp, err := m.get(id)
	if err != nil {
		return err
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()

	switch mp.info.State {
	case StateRunning, StateStarting, StateStopping:
		return ErrAlreadyRunning
	case StateCrashLoop:
		return ErrCrashLoop
	}
	return mp.startLocked()
}

// startLocked 启动进程,调用方须持有 mp.mu。
func (mp *ManagedPlugin) startLocked() error {
	mp.info.State = StateStarting
	mp.info.LastErr = ""
	// 注意:不清零 Restarts。崩溃计数在 watchExit 中累积,
	// 只有主动 Stop(人工介入)才清零,保证 crash-loop 能正确触发。
	mp.done = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	mp.cancel = cancel
	// CommandContext:ctx 取消时自动杀进程(跨平台)
	cmd := exec.CommandContext(ctx, mp.info.Path, "--name="+mp.info.ID, "--port=0")
	// 输出透传(P2 改为解析注册 JSON)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		mp.cmd = nil
		mp.cancel = nil
		mp.info.State = StateStopped
		mp.info.LastErr = err.Error()
		return fmt.Errorf("%w: %v", ErrStartFailed, err)
	}
	mp.cmd = cmd
	mp.info.State = StateRunning
	go mp.watchExit(cmd)
	return nil
}

// watchExit 监控进程退出:区分主动停止(stopping)与崩溃(crashed)。
func (mp *ManagedPlugin) watchExit(cmd *exec.Cmd) {
	_ = cmd.Wait()
	close(mp.done)

	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.cmd != cmd { // 已被 Stop 兜底清理,状态已置
		return
	}
	mp.cmd = nil
	mp.cancel = nil

	switch mp.info.State {
	case StateStopping:
		mp.info.State = StateStopped
		mp.info.Restarts = 0
	default: // running/starting 期间退出 = 崩溃
		mp.info.State = StateCrashed
		mp.info.Restarts++
		mp.info.LastErr = fmt.Sprintf("进程异常退出,连续 %d 次", mp.info.Restarts)
		if mp.info.Restarts >= maxRestarts {
			mp.info.State = StateCrashLoop
		}
	}
}

// Stop 停止插件进程(主动停止,不触发崩溃计数)。
// crashed / crash-loop 状态视为已停止,复位为 stopped(人工介入入口)。
func (m *Manager) Stop(id string) error {
	mp, err := m.get(id)
	if err != nil {
		return err
	}
	mp.mu.Lock()
	state := mp.info.State
	switch state {
	case StateStopped:
		mp.mu.Unlock()
		return ErrNotRunning
	case StateCrashed, StateCrashLoop:
		// 进程已退出,直接复位(允许从 crash-loop 恢复)
		mp.cmd = nil
		mp.cancel = nil
		mp.info.State = StateStopped
		mp.info.Restarts = 0
		mp.info.LastErr = ""
		mp.mu.Unlock()
		return nil
	}
	mp.info.State = StateStopping
	mp.cancel()
	done := mp.done
	cmd := mp.cmd
	mp.mu.Unlock()

	// 等待进程退出(不持锁,让 watchExit 能拿到锁更新状态)
	select {
	case <-done:
	case <-time.After(stopWaitTimeout):
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second): // 兜底,不再等待
		}
	}
	// 兜底清理(watchExit 正常路径已置 stopped)
	mp.mu.Lock()
	if mp.info.State == StateStopping {
		mp.cmd = nil
		mp.cancel = nil
		mp.info.State = StateStopped
		mp.info.Restarts = 0
	}
	mp.mu.Unlock()
	return nil
}

// Restart 重启插件(停止 → 启动)。
func (m *Manager) Restart(id string) error {
	if _, err := m.get(id); err != nil {
		return err
	}
	if err := m.Stop(id); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	return m.Start(id)
}

// ShutdownAll 停止全部插件(daemon 优雅退出时调用)。
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.plugins))
	for id := range m.plugins {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		if err := m.Stop(id); err == nil {
			m.log.Info("插件已停止", "id", id)
		}
	}
}
