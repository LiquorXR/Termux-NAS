package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
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
