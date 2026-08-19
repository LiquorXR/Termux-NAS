package daemon

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// newTestRequest 构造 fiber app.Test 可用的 HTTP 请求。
func newTestRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	return req
}

// newTestDaemonHTTP 构建仅含插件反代路由的 fiber app(不经完整 Daemon 启动)。
// 用于端到端验证:懒加载 → 注册 → 反代 → 响应。
func newTestDaemonHTTP(t *testing.T) (*fiber.App, *Manager, string) {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(pluginsDir, logger)
	t.Cleanup(m.ShutdownAll)

	d := &Daemon{pm: m, log: logger}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.All("/p/:id/*", d.pluginProxy)
	app.All("/p/:id", d.pluginProxy)
	return app, m, pluginsDir
}

func TestProxyEndToEnd(t *testing.T) {
	app, m, pluginsDir := newTestDaemonHTTP(t)
	name := buildHelper(t, pluginsDir)
	if _, err := m.Scan(); err != nil {
		t.Fatal(err)
	}

	// 首次访问触发懒加载 + 注册 + 反代
	resp, err := app.Test(newTestRequest("GET", "/p/"+name+"/hello", ""), 10*1000)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("状态码 = %d,期望 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	want := `{"plugin":` + `"` + name + `"` + `,"path":"/hello"}`
	if string(body) != want {
		t.Errorf("响应体 = %s,期望 %s", body, want)
	}

	// 插件应处于运行状态且已注册
	info, err := m.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.State != StateRunning || info.Reg == nil {
		t.Fatalf("代理后插件应 running 且已注册,state=%s reg=%v", info.State, info.Reg)
	}

	// 健康检查端点透传
	resp2, err := app.Test(newTestRequest("GET", "/p/"+name+"/health", ""), 5*1000)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != 200 || string(b2) != "ok" {
		t.Errorf("health 透传失败: %d %s", resp2.StatusCode, b2)
	}
}

func TestProxyUnknownPlugin(t *testing.T) {
	app, _, _ := newTestDaemonHTTP(t)
	resp, err := app.Test(newTestRequest("GET", "/p/ghost/hello", ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("未知插件应返回 404,得到 %d", resp.StatusCode)
	}
}

func TestProxyInvalidID(t *testing.T) {
	app, _, _ := newTestDaemonHTTP(t)
	resp, err := app.Test(newTestRequest("GET", "/p/..%2Fevil/hello", ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("非法 ID 应返回 400,得到 %d", resp.StatusCode)
	}
}
