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

func TestCrashCountsAndCrashLoop(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}

	// 让插件启动即崩溃
	t.Setenv("NAS_HELPER_CRASH", "1")

	for i := 1; i <= maxRestarts; i++ {
		if err := m.Start(name); err != nil {
			t.Fatalf("第 %d 次 Start: %v", i, err)
		}
		// 等待崩溃被 watchExit 检测到
		info, _ := m.Get(name)
		eventually(t, 3*time.Second, func() bool {
			info, _ = m.Get(name)
			return info.State == StateCrashed || info.State == StateCrashLoop
		}, "进程崩溃状态")
		info, _ = m.Get(name)
		if info.Restarts != i {
			t.Errorf("第 %d 次崩溃后 Restarts = %d", i, info.Restarts)
		}
		if i < maxRestarts && info.State != StateCrashed {
			t.Errorf("第 %d 次应处于 crashed,得到 %s", i, info.State)
		}
		if i == maxRestarts && info.State != StateCrashLoop {
			t.Errorf("第 %d 次应进入 crash-loop,得到 %s", i, info.State)
		}
	}

	// crash-loop 状态下禁止启动
	if err := m.Start(name); err != ErrCrashLoop {
		t.Errorf("crash-loop 下 Start 应返回 ErrCrashLoop,得到 %v", err)
	}
}

func TestCrashThenManualRestartResets(t *testing.T) {
	m, pluginsDir := newTestManager(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}

	// 崩溃一次
	t.Setenv("NAS_HELPER_CRASH", "1")
	if err := m.Start(name); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		info, _ := m.Get(name)
		return info.State == StateCrashed
	}, "崩溃状态")

	// 手动 Stop 复位(容忍 ErrNotRunning,因进程已退出)
	_ = m.Stop(name)
	info, _ := m.Get(name)
	if info.State != StateStopped || info.Restarts != 0 {
		t.Errorf("手动复位后应为 stopped/restarts=0,得到 %s/%d", info.State, info.Restarts)
	}

	// 恢复正常插件,可再次启动
	t.Setenv("NAS_HELPER_CRASH", "")
	if err := m.Start(name); err != nil {
		t.Fatalf("复位后重新 Start: %v", err)
	}
	eventually(t, 3*time.Second, func() bool {
		info, _ := m.Get(name)
		return info.State == StateRunning
	}, "重新运行")
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
