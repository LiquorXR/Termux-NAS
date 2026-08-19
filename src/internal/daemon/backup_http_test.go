package daemon

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/backup"
	_ "modernc.org/sqlite"
)

// newTestBackupApp 构建含备份 API 的 fiber app(内存 SQLite + 临时目录)。
func newTestBackupApp(t *testing.T) (*fiber.App, *backup.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := backup.NewStore(db, logger)
	if err != nil {
		t.Fatal(err)
	}
	m := backup.NewManager(store, logger, nil)
	d := &Daemon{backups: m, log: logger}

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/api/backup/jobs", d.backupJobs)
	app.Post("/api/backup/jobs", d.backupCreate)
	app.Delete("/api/backup/jobs/:id", d.backupDelete)
	app.Post("/api/backup/run", d.backupRun)
	return app, m, dir
}

func TestBackupJobsCRUD(t *testing.T) {
	app, _, _ := newTestBackupApp(t)
	// 创建
	resp, err := app.Test(newBackupRequest(t, "POST", "/api/backup/jobs",
		`{"name":"照片","source":"/s","target":"/d","schedule":"0 2 * * *"}`), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("创建返回 %d: %s", resp.StatusCode, b)
	}
	// 列表
	resp, _ = app.Test(newTestRequest("GET", "/api/backup/jobs", ""), 3*1000)
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "照片") {
		t.Errorf("列表应含任务,得到 %s", body)
	}
	// 缺参校验
	resp, _ = app.Test(newBackupRequest(t, "POST", "/api/backup/jobs", `{"name":"x"}`), 3*1000)
	if resp.StatusCode != 400 {
		t.Errorf("缺 source 应返回 400,得到 %d", resp.StatusCode)
	}
}

func TestBackupRunAndStatus(t *testing.T) {
	app, _, dir := newTestBackupApp(t)
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 创建任务(Windows 路径转义反斜杠)
	escSrc := strings.ReplaceAll(src, `\`, `\\`)
	escDst := strings.ReplaceAll(dst, `\`, `\\`)
	resp, _ := app.Test(newBackupRequest(t, "POST", "/api/backup/jobs",
		`{"name":"测试","source":"`+escSrc+`","target":"`+escDst+`"}`), 3*1000)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("创建失败 %d: %s", resp.StatusCode, body)
	}
	// 解析 ID
	idStr := extractJSONID(string(body))
	// 执行
	resp, _ = app.Test(newBackupRequest(t, "POST", "/api/backup/run", `{"id":`+idStr+`}`), 3*1000)
	if resp.StatusCode != 200 {
		t.Fatalf("执行返回 %d", resp.StatusCode)
	}
	// 目标目录应被创建(异步执行,轮询等待)
	deadline := 0
	for deadline < 50 {
		if _, err := os.Stat(filepath.Join(dst, "a.txt")); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
		deadline++
	}
	if deadline >= 50 {
		t.Error("备份后目标文件未出现")
	}
}

// extractJSONID 从 {"job":{"id":N}} 响应提取 ID。
func extractJSONID(body string) string {
	idx := strings.Index(body, `"id":`)
	if idx < 0 {
		return "0"
	}
	rest := body[idx+5:]
	end := strings.IndexByte(rest, ',')
	if end < 0 {
		end = strings.IndexByte(rest, '}')
	}
	if end < 0 {
		return "0"
	}
	return strings.TrimSpace(rest[:end])
}

// newBackupRequest JSON POST 请求。
func newBackupRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	req := newTestRequest(method, target, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}
