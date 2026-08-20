package auth

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
	_ "modernc.org/sqlite"
)

// newTestStore 内存 SQLite 认证存储(复刻 daemon 迁移 v2 的表结构)。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);`); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return NewStore(db, logger)
}

func TestCreateUserAndAuthenticate(t *testing.T) {
	s := newTestStore(t)
	if has, _ := s.HasUsers(); has {
		t.Fatal("初始不应有用户")
	}
	if err := s.CreateUser("admin", "password123"); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasUsers(); !has {
		t.Fatal("创建后应有用户")
	}
	// 重复用户名
	if err := s.CreateUser("admin", "other123"); err != ErrUserExists {
		t.Errorf("重复用户名应返回 ErrUserExists,得到 %v", err)
	}
	// 正确凭据
	u, err := s.Authenticate("admin", "password123")
	if err != nil || u.Username != "admin" {
		t.Fatalf("正确凭据应认证成功: %v %+v", err, u)
	}
	// 错误密码 / 不存在用户:统一 ErrBadCreds(防用户枚举)
	for _, cred := range []struct{ u, p string }{
		{"admin", "wrong"},
		{"nobody", "password123"},
	} {
		if _, err := s.Authenticate(cred.u, cred.p); err != ErrBadCreds {
			t.Errorf("Authenticate(%s) 应返回 ErrBadCreds,得到 %v", cred.u, err)
		}
	}
	// 用户名空白修整
	if _, err := s.Authenticate("  admin  ", "password123"); err != nil {
		t.Errorf("用户名应 TrimSpace 后匹配: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateUser("u", "password123"); err != nil {
		t.Fatal(err)
	}
	u, _ := s.Authenticate("u", "password123")

	sess, err := s.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Token) != 64 { // 32 字节 hex
		t.Errorf("token 应为 64 位 hex,得到 %d", len(sess.Token))
	}
	// 有效会话
	got, err := s.GetSession(sess.Token)
	if err != nil || got.UserID != u.ID {
		t.Fatalf("GetSession 应成功: %v %+v", err, got)
	}
	// 未知 token
	if _, err := s.GetSession("deadbeef"); err != ErrNoSession {
		t.Errorf("未知 token 应返回 ErrNoSession,得到 %v", err)
	}
	// 登出删除
	if err := s.DeleteSession(sess.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(sess.Token); err != ErrNoSession {
		t.Errorf("删除后应 ErrNoSession,得到 %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateUser("u", "password123"); err != nil {
		t.Fatal(err)
	}
	u, _ := s.Authenticate("u", "password123")
	token, _ := randomToken(32)
	// 直接写入已过期会话
	if _, err := s.db.Exec(
		`INSERT INTO sessions(token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, u.ID, time.Now().Format(time.RFC3339),
		time.Now().Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(token); err != ErrNoSession {
		t.Errorf("过期会话应返回 ErrNoSession,得到 %v", err)
	}
	// 过期会话应被自动清理
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token = ?`, token).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("过期会话应被自动删除")
	}
}

// newCtxForCookie 构造无路由 fiber ctx(供 cookie 属性断言)。
func newCtxForCookie(t *testing.T) *fiber.Ctx {
	t.Helper()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var rctx fasthttp.RequestCtx
	rctx.Init(&fasthttp.Request{}, nil, nil)
	ctx := app.AcquireCtx(&rctx)
	t.Cleanup(func() { app.ReleaseCtx(ctx) })
	return ctx
}

// TestSetSessionCookieFlags 会话 cookie 属性回归(S6):
// HttpOnly + SameSite=Lax + Max-Age 对齐 7 天 TTL;Secure 由部署模式决定。
// 注:fasthttp 序列化 cookie 属性为小写(max-age/secure),断言须大小写不敏感。
func TestSetSessionCookieFlags(t *testing.T) {
	check := func(secure bool) string {
		c := newCtxForCookie(t)
		SetSessionCookie(c, "tok123", secure)
		raw := c.Response().Header.Peek(fiber.HeaderSetCookie)
		if raw == nil {
			t.Fatal("应下发 nas_session cookie")
		}
		return strings.ToLower(string(raw))
	}
	s := check(false)
	for _, want := range []string{"httponly", "samesite=lax", "max-age=604800"} {
		if !strings.Contains(s, want) {
			t.Errorf("cookie 应含 %s,得到 %s", want, s)
		}
	}
	if strings.Contains(s, "secure") {
		t.Error("非 HTTPS 模式下 cookie 不应含 secure")
	}
	if s2 := check(true); !strings.Contains(s2, "secure") {
		t.Error("HTTPS 模式下 cookie 应含 secure")
	}
}

// TestPasswordHashRoundtrip 密码哈希往返。
func TestPasswordHashRoundtrip(t *testing.T) {
	h, err := hashPassword("s3cret-password")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := verifyPassword("s3cret-password", h)
	if err != nil || !ok {
		t.Fatalf("正确密码应验证通过: %v", err)
	}
	ok, _ = verifyPassword("wrong", h)
	if ok {
		t.Fatal("错误密码不应通过")
	}
	// 非法格式
	if _, err := verifyPassword("x", "not-a-hash"); err == nil {
		t.Fatal("非法哈希格式应报错")
	}
}
