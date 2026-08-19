package daemon

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/svc"
)

// newTestSvcApp 构建含服务控制 API 的 fiber app(注入假执行器)。
func newTestSvcApp(t *testing.T) *fiber.App {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// 用 MockRunner 保证测试跨平台可跑
	d := &Daemon{svc: svc.New(nil, svc.NewMockRunner(), logger), log: logger}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/api/svc/list", d.svcList)
	app.Post("/api/svc/start", d.svcStart)
	app.Post("/api/svc/stop", d.svcStop)
	app.Post("/api/svc/restart", d.svcRestart)
	app.Post("/api/svc/autostart", d.svcAutostart)
	return app
}

func TestSvcList(t *testing.T) {
	app := newTestSvcApp(t)
	resp, err := app.Test(newTestRequest("GET", "/api/svc/list", ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "sshd") {
		t.Errorf("列表应包含 sshd,得到 %s", body)
	}
}

// jsonRequest 构造带 JSON Content-Type 的 POST 请求。
func jsonRequest(t *testing.T, target, body string) *http.Request {
	t.Helper()
	req := newTestRequest("POST", target, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestSvcStartStop(t *testing.T) {
	app := newTestSvcApp(t)
	// 启动
	resp, err := app.Test(jsonRequest(t, "/api/svc/start", `{"name":"sshd"}`), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("启动返回 %d: %s", resp.StatusCode, b)
	}
	// 停止
	resp, err = app.Test(jsonRequest(t, "/api/svc/stop", `{"name":"sshd"}`), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("停止返回 %d", resp.StatusCode)
	}
}

func TestSvcAutostart(t *testing.T) {
	app := newTestSvcApp(t)
	resp, err := app.Test(jsonRequest(t, "/api/svc/autostart", `{"name":"sshd","enabled":true}`), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("设置自启返回 %d: %s", resp.StatusCode, b)
	}
}

func TestSvcUnknownService(t *testing.T) {
	app := newTestSvcApp(t)
	resp, err := app.Test(jsonRequest(t, "/api/svc/start", `{"name":"ghost"}`), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("未知服务应返回 404,得到 %d: %s", resp.StatusCode, b)
	}
}

func TestSvcMissingName(t *testing.T) {
	app := newTestSvcApp(t)
	resp, err := app.Test(jsonRequest(t, "/api/svc/start", `{}`), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("缺少 name 应返回 400,得到 %d", resp.StatusCode)
	}
}
