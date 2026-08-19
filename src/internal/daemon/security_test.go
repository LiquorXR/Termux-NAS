package daemon

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestSecurityHeaders(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(securityHeaders)
	app.Get("/api/test", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/login", func(c *fiber.Ctx) error { return c.SendString("login") })

	// API 响应带安全头
	resp, err := app.Test(newTestRequest("GET", "/api/test", ""), 3*1000)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Content-Security-Policy": "default-src 'self'",
	}
	for h, prefix := range checks {
		got := resp.Header.Get(h)
		if got == "" || got[:len(prefix)] != prefix {
			t.Errorf("头 %s = %q,期望以 %q 开头", h, got, prefix)
		}
	}

	// 登录页禁止缓存
	resp2, _ := app.Test(newTestRequest("GET", "/login", ""), 3*1000)
	if cc := resp2.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("登录页 Cache-Control = %q,期望 no-store", cc)
	}
	// 普通页面不禁缓存
	resp3, _ := app.Test(newTestRequest("GET", "/api/test", ""), 3*1000)
	if cc := resp3.Header.Get("Cache-Control"); cc == "no-store" {
		t.Error("API 响应不应有 no-store")
	}
}
