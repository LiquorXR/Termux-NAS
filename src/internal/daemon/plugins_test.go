package daemon

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// buildHelper 将 testdata/helper 编译为临时 plugins 目录下的可执行文件。
// 返回插件文件名(跨平台:Windows 需 .exe 后缀才有执行位)。
func buildHelper(t *testing.T, pluginsDir string) string {
	t.Helper()
	name := "helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(pluginsDir, name)
	cmd := exec.Command("go", "build", "-o", out, "./testdata/helper")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译测试插件失败: %v\n%s", err, b)
	}
	return name
}

// newTestManager 创建指向临时目录的 Manager,并注册测试结束时的清理。
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	m := NewManager(pluginsDir, logger)
	// Windows 上运行中的 exe 文件无法删除,测试结束前必须停止所有插件
	t.Cleanup(m.ShutdownAll)
	return m, pluginsDir
}

// eventually 轮询断言,等待异步状态(如 watchExit)收敛。
func eventually(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("超时等待: " + msg)
}

func TestScanEmptyDir(t *testing.T) {
	m, _ := newTestManager(t)
	list, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("空目录应登记 0 个插件,得到 %d", len(list))
	}
}

func TestScanRegistersExecutables(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)

	// 放入一个不可执行文件,应被忽略
	ignored := filepath.Join(pluginsDir, "not_exec")
	if err := os.WriteFile(ignored, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("应登记 1 个插件(helper),得到 %d: %+v", len(list), list)
	}
	if list[0].ID != name {
		t.Errorf("插件 ID = %q,期望 %q", list[0].ID, name)
	}
	if list[0].State != StateStopped {
		t.Errorf("初始状态应为 stopped,得到 %s", list[0].State)
	}
}

func TestStartStop(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}

	if err := m.Start(name); err != nil {
		t.Fatalf("Start: %v", err)
	}
	info, _ := m.Get(name)
	if info.State != StateRunning {
		t.Fatalf("启动后状态应为 running,得到 %s", info.State)
	}
	if info.PID <= 0 {
		t.Errorf("PID 应为正数,得到 %d", info.PID)
	}

	// 重复启动应报错
	if err := m.Start(name); err != ErrAlreadyRunning {
		t.Errorf("重复启动应返回 ErrAlreadyRunning,得到 %v", err)
	}

	if err := m.Stop(name); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	info, _ = m.Get(name)
	if info.State != StateStopped {
		t.Fatalf("停止后状态应为 stopped,得到 %s", info.State)
	}

	// 再次停止应报错
	if err := m.Stop(name); err != ErrNotRunning {
		t.Errorf("重复停止应返回 ErrNotRunning,得到 %v", err)
	}
}

func TestRestart(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(name); err != nil {
		t.Fatal(err)
	}
	if err := m.Restart(name); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	info, _ := m.Get(name)
	if info.State != StateRunning {
		t.Fatalf("重启后状态应为 running,得到 %s", info.State)
	}
}

func TestNotFound(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start("ghost"); err != ErrPluginNotFound {
		t.Errorf("启动不存在插件应返回 ErrPluginNotFound,得到 %v", err)
	}
	if err := m.Stop("ghost"); err != ErrPluginNotFound {
		t.Errorf("停止不存在插件应返回 ErrPluginNotFound,得到 %v", err)
	}
}

func TestShutdownAll(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(name); err != nil {
		t.Fatal(err)
	}
	m.ShutdownAll()
	info, _ := m.Get(name)
	if info.State != StateStopped {
		t.Fatalf("ShutdownAll 后应为 stopped,得到 %s", info.State)
	}
}

// --- P2 注册协议 + 懒加载 + 空闲回收 ---

func TestStartRegisters(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(name); err != nil {
		t.Fatalf("Start: %v", err)
	}
	info, _ := m.Get(name)
	if info.State != StateRunning {
		t.Fatalf("注册成功后状态应为 running,得到 %s", info.State)
	}
	if info.Reg == nil {
		t.Fatal("应登记注册信息 Reg")
	}
	if info.Reg.ID != name {
		t.Errorf("Reg.ID = %q,期望 %q", info.Reg.ID, name)
	}
	if info.Reg.Port <= 0 {
		t.Errorf("Reg.Port 应为正数,得到 %d", info.Reg.Port)
	}
	_ = m.Stop(name)
}

func TestStartRegTimeout(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	// 插件不输出注册 JSON → 注册超时,启动失败
	t.Setenv("NAS_HELPER_NO_REG", "1")
	if err := m.Start(name); err == nil {
		t.Fatal("不输出注册信息时 Start 应报错")
	}
	// 注册超时(5s)杀进程 → watchExit 按崩溃处理 → crashed/crash-loop
	eventually(t, 10*time.Second, func() bool {
		info, _ := m.Get(name)
		return info.State == StateCrashed || info.State == StateCrashLoop
	}, "注册失败后状态收敛")
}

func TestEnsureRunningLazyStart(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	// 未启动时 EnsureRunning 应懒加载启动并完成注册
	info, err := m.EnsureRunning(name)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if info.State != StateRunning {
		t.Fatalf("懒加载后应为 running,得到 %s", info.State)
	}
	if info.Reg == nil || info.Reg.Port <= 0 {
		t.Fatalf("懒加载后应完成注册,Reg=%+v", info.Reg)
	}
	// 再次调用应复用运行中实例
	info2, err := m.EnsureRunning(name)
	if err != nil {
		t.Fatal(err)
	}
	if info2.PID != info.PID {
		t.Errorf("二次 EnsureRunning 应复用同一进程,PID %d vs %d", info.PID, info2.PID)
	}
	_ = m.Stop(name)
}

func TestReapIdle(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(name); err != nil {
		t.Fatal(err)
	}
	// 主动 Touch 一次(模拟访问),然后等 idle=0 回收
	m.Touch(name)
	time.Sleep(20 * time.Millisecond)
	reaped := m.Reap(10 * time.Millisecond)
	if len(reaped) != 1 || reaped[0] != name {
		t.Fatalf("应回收 %q,得到 %v", name, reaped)
	}
	info, _ := m.Get(name)
	if info.State != StateStopped {
		t.Fatalf("回收后应为 stopped,得到 %s", info.State)
	}
}

func TestAutoRestartAfterCrash(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}
	// 插件启动即崩溃 → Start 返回注册失败(允许),watchExit 自动重启(带退避)
	t.Setenv("NAS_HELPER_CRASH", "1")
	_ = m.Start(name) // 崩溃插件注册失败,错误可忽略
	// 连续崩溃应最终进入 crash-loop(自动重启 3 次后停止)
	eventually(t, 20*time.Second, func() bool {
		info, _ := m.Get(name)
		return info.State == StateCrashLoop
	}, "崩溃循环后进入 crash-loop")
	info, _ := m.Get(name)
	if info.Restarts < maxRestarts {
		t.Errorf("Restarts 应 >= %d,得到 %d", maxRestarts, info.Restarts)
	}
	// crash-loop 下 Start 应被拒绝
	if err := m.Start(name); err != ErrCrashLoop {
		t.Errorf("crash-loop 下 Start 应返回 ErrCrashLoop,得到 %v", err)
	}
	// Stop 复位后可重新启动
	t.Setenv("NAS_HELPER_CRASH", "")
	if err := m.Stop(name); err != nil {
		t.Fatalf("Stop 复位: %v", err)
	}
	if err := m.Start(name); err != nil {
		t.Fatalf("复位后 Start: %v", err)
	}
}
