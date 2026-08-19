package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// newTestMarketApp 构建含市场 API 的 fiber app(空插件目录)。
func newTestMarketApp(t *testing.T) *fiber.App {
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
	app.Get("/api/market", d.marketIndex)
	app.Post("/api/market/install", d.marketInstall)
	return app
}

func TestMarketIndex(t *testing.T) {
	app := newTestMarketApp(t)
	resp, err := app.Test(newTestRequest("GET", "/api/market", ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{"download", "alist", "media", "photos", "installed"} {
		if !strings.Contains(s, want) {
			t.Errorf("市场响应应包含 %q", want)
		}
	}
}

func TestMarketInstallUnknownPlugin(t *testing.T) {
	app := newTestMarketApp(t)
	resp, err := app.Test(newBackupRequest(t, "POST", "/api/market/install", `{"id":"ghost"}`), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("未知市场插件应 404,得到 %d", resp.StatusCode)
	}
}
