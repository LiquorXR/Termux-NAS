package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/config"
)

// makePluginPkg 生成含单个可执行文件的 .tar.gz 包(模拟插件安装包)。
func makePluginPkg(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name: "plugin.bin",
		Mode: 0o755,
		Size: int64(len(payload)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newPluginsDaemon 构造仅含插件目录的轻量 Daemon(供 API 测试)。
func newPluginsDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(pluginsDir, logger)
	t.Cleanup(m.ShutdownAll)
	return &Daemon{pm: m, paths: config.Paths{Plugins: pluginsDir}, log: logger}, pluginsDir
}

// installName 返回当前平台可被扫描到的插件名(Windows 需 .exe 后缀)。
func installName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func TestInstallPluginFromReader(t *testing.T) {
	d, _ := newPluginsDaemon(t)
	pkg := makePluginPkg(t, []byte("#!/bin/sh\necho hello\n"))
	name := installName("hello")
	if _, err := d.installPluginFromReader(name, bytes.NewReader(pkg)); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	// 文件已写入且可执行
	fi, err := os.Stat(filepath.Join(d.paths.Plugins, name))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() == 0 {
		t.Error("插件文件为空")
	}
	// 已登记到管理器
	info, err := d.pm.Get(name)
	if err != nil {
		t.Fatalf("安装后未登记: %v", err)
	}
	if info.State != StateStopped {
		t.Errorf("新装插件应为 stopped,得到 %s", info.State)
	}
}

func TestInstallPluginRejectsBadPackage(t *testing.T) {
	d, _ := newPluginsDaemon(t)
	// 非 gzip 数据
	if _, err := d.installPluginFromReader("bad", bytes.NewReader([]byte("not gzip"))); err == nil {
		t.Fatal("非 gzip 包应报错")
	}
	// 空 tar(无可执行文件)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_ = gz.Close()
	if _, err := d.installPluginFromReader("empty", bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("空包应报错")
	}
	// 非法插件名
	pkg := makePluginPkg(t, []byte("x"))
	if _, err := d.installPluginFromReader("../evil", bytes.NewReader(pkg)); err == nil {
		t.Fatal("非法插件名应被拒绝")
	}
}

func TestPluginUninstall(t *testing.T) {
	d, _ := newPluginsDaemon(t)
	name := installName("temp")
	pkg := makePluginPkg(t, []byte("x"))
	if _, err := d.installPluginFromReader(name, bytes.NewReader(pkg)); err != nil {
		t.Fatal(err)
	}
	// 卸载
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Delete("/api/plugins/:id", d.pluginUninstall)
	resp, err := app.Test(newTestRequest("DELETE", "/api/plugins/"+name, ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("卸载返回 %d: %s", resp.StatusCode, b)
	}
	// 文件已删除,登记已注销
	if _, err := os.Stat(filepath.Join(d.paths.Plugins, name)); !os.IsNotExist(err) {
		t.Error("卸载后文件应被删除")
	}
	if _, err := d.pm.Get(name); err != ErrPluginNotFound {
		t.Errorf("卸载后应注销登记,得到 %v", err)
	}
}

func TestPluginAPIStartStop(t *testing.T) {
	d, pluginsDir := newPluginsDaemon(t)
	name := buildHelper(t, pluginsDir)
	if _, err := d.pm.Scan(); err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Post("/api/plugins/:id/start", d.pluginStart)
	app.Post("/api/plugins/:id/stop", d.pluginStop)

	// 启动
	resp, err := app.Test(newTestRequest("POST", "/api/plugins/"+name+"/start", ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("启动返回 %d", resp.StatusCode)
	}
	info, _ := d.pm.Get(name)
	if info.State != StateRunning {
		t.Fatalf("启动后应为 running,得到 %s", info.State)
	}
	// 停止
	resp, err = app.Test(newTestRequest("POST", "/api/plugins/"+name+"/stop", ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("停止返回 %d", resp.StatusCode)
	}
	info, _ = d.pm.Get(name)
	if info.State != StateStopped {
		t.Fatalf("停止后应为 stopped,得到 %s", info.State)
	}
}
