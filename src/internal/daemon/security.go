package daemon

import (
	"github.com/gofiber/fiber/v2"
)

// securityHeaders 安全响应头中间件(M5 安全加固)。
// 注意:HTMX 依赖内联脚本,故 CSP 不放宽 script-src 时页面会失效,
// 当前仅收紧 default-src 'self'(同源),script-src 允许内联(HTMX 事件处理器)。
func securityHeaders(c *fiber.Ctx) error {
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "DENY")
	c.Set("Referrer-Policy", "no-referrer")
	c.Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
	// 登录/设置页避免被浏览器缓存(含会话 cookie 的响应)
	if c.Path() == "/login" || c.Path() == "/setup" {
		c.Set("Cache-Control", "no-store")
	}
	return c.Next()
}
