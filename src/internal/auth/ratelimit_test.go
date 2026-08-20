package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

// req 构造 POST JSON 请求。
func req(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// newLimiterTestApp 构建含登录路由的 fiber app(仅测限流,不依赖 DB)。
func newLimiterTestApp() (*fiber.App, *loginLimiter) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	limiter := newLoginLimiter()
	// 模拟登录 handler:先 allow,再模拟失败/成功
	app.Post("/login", func(c *fiber.Ctx) error {
		if !limiter.allow(c) {
			ra := limiter.retryAfter(c)
			if ra > 0 {
				c.Set("Retry-After", strconv.Itoa(ra))
			}
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "限流"})
		}
		// 读 body 判断成功/失败
		var body struct {
			Ok bool `json:"ok"`
		}
		_ = c.BodyParser(&body)
		if body.Ok {
			limiter.success(c)
			return c.JSON(fiber.Map{"ok": true})
		}
		limiter.fail(c)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "bad"})
	})
	return app, limiter
}

func TestLoginLimiterBlocksAfterMaxFails(t *testing.T) {
	app, _ := newLimiterTestApp()
	// 连续失败 maxFails 次
	for i := 0; i < 5; i++ {
		resp, err := app.Test(req("POST", "/login", `{"ok":false}`))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 401 {
			t.Fatalf("第 %d 次失败应返回 401,得到 %d", i+1, resp.StatusCode)
		}
	}
	// 第 6 次应被限流(429)
	resp, _ := app.Test(req("POST", "/login", `{"ok":false}`))
	if resp.StatusCode != 429 {
		t.Fatalf("超过阈值应返回 429,得到 %d", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Error("限流响应应带 Retry-After 头")
	}
}

func TestLoginLimiterSuccessResets(t *testing.T) {
	app, _ := newLimiterTestApp()
	// 失败 3 次后成功 1 次(清零),再失败应正常计数
	for i := 0; i < 3; i++ {
		app.Test(req("POST", "/login", `{"ok":false}`))
	}
	resp, _ := app.Test(req("POST", "/login", `{"ok":true}`))
	if resp.StatusCode != 200 {
		t.Fatalf("成功登录应 200,得到 %d", resp.StatusCode)
	}
	// 清零后再失败 5 次 → 第 5 次失败后第 6 次应 429
	for i := 0; i < 5; i++ {
		app.Test(req("POST", "/login", `{"ok":false}`))
	}
	resp, _ = app.Test(req("POST", "/login", `{"ok":false}`))
	if resp.StatusCode != 429 {
		t.Fatalf("清零后重新累积应 429,得到 %d", resp.StatusCode)
	}
}

// TestLoginLimiterXFFSpoof (S3 回归):默认不信任 XFF,伪造头无法绕过限流。
func TestLoginLimiterXFFSpoof(t *testing.T) {
	app, _ := newLimiterTestApp()
	spoof := func() *http.Request {
		r := req("POST", "/login", `{"ok":false}`)
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		return r
	}
	for i := 0; i < 5; i++ {
		if resp, _ := app.Test(spoof()); resp.StatusCode != 401 {
			t.Fatalf("第 %d 次失败应 401,得到 %d", i+1, resp.StatusCode)
		}
	}
	// 伪造不同 XFF 也应被同一远端地址累计 → 429
	if resp, _ := app.Test(spoof()); resp.StatusCode != 429 {
		t.Fatalf("XFF 伪造不应绕过限流,应 429,得到 %d", resp.StatusCode)
	}
}

// TestLoginLimiterTrustXFF (S3):显式开启信任后,按 XFF 维度计数。
func TestLoginLimiterTrustXFF(t *testing.T) {
	app, limiter := newLimiterTestApp()
	limiter.setTrustXFF(true)
	for i := 0; i < 5; i++ {
		r := req("POST", "/login", `{"ok":false}`)
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		if resp, _ := app.Test(r); resp.StatusCode != 401 {
			t.Fatalf("第 %d 次失败应 401,得到 %d", i+1, resp.StatusCode)
		}
	}
	r := req("POST", "/login", `{"ok":false}`)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if resp, _ := app.Test(r); resp.StatusCode != 429 {
		t.Fatalf("信任 XFF 后同源应 429,得到 %d", resp.StatusCode)
	}
	// 多级 XFF 取最左侧(原始客户端)
	r = req("POST", "/login", `{"ok":false}`)
	r.Header.Set("X-Forwarded-For", "203.0.113.99, 10.0.0.1")
	if resp, _ := app.Test(r); resp.StatusCode != 401 {
		t.Fatalf("不同 XFF 客户端不应被锁定,应 401,得到 %d", resp.StatusCode)
	}
}

// TestLoginLimiterSlidingWindow (S4 回归):窗口外失败重置计数,不触发锁定。
func TestLoginLimiterSlidingWindow(t *testing.T) {
	app, limiter := newLimiterTestApp()
	limiter.window = time.Second
	limiter.maxFails = 5
	// 4 次失败(未达阈值)
	for i := 0; i < 4; i++ {
		if resp, _ := app.Test(req("POST", "/login", `{"ok":false}`)); resp.StatusCode != 401 {
			t.Fatalf("失败应 401,得到 %d", resp.StatusCode)
		}
	}
	// 超过窗口:计数重置,第 5 次失败不应锁定
	time.Sleep(1100 * time.Millisecond)
	resp, _ := app.Test(req("POST", "/login", `{"ok":false}`))
	if resp.StatusCode != 401 {
		t.Fatalf("窗口滑动后应重置计数(401),得到 %d", resp.StatusCode)
	}
}

// TestLoginLimiterMapCleanup (S4 回归):锁定解除后条目被清理,map 不无限增长。
func TestLoginLimiterMapCleanup(t *testing.T) {
	_, limiter := newLimiterTestApp()
	limiter.maxKeys = 10
	limiter.lockFor = time.Second
	// 触发锁定
	for i := 0; i < limiter.maxFails; i++ {
		limiter.fail(testCtx(t))
	}
	if len(limiter.locked) == 0 {
		t.Fatal("应已锁定")
	}
	// 等待锁定过期后 allow 应解除并清空失败记录
	time.Sleep(1100 * time.Millisecond)
	limiter.allow(testCtx(t))
	limiter.pruneIfNeeded(time.Now())
	if len(limiter.locked) != 0 || len(limiter.fails) != 0 {
		t.Errorf("过期条目应被清理: locked=%d fails=%d", len(limiter.locked), len(limiter.fails))
	}
}

// testCtx 构造无路由 ctx(限流 key 取默认远端)。
func testCtx(t testing.TB) *fiber.Ctx {
	t.Helper()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var rctx fasthttp.RequestCtx
	rctx.Init(&fasthttp.Request{}, nil, nil)
	ctx := app.AcquireCtx(&rctx)
	t.Cleanup(func() { app.ReleaseCtx(ctx) })
	return ctx
}
